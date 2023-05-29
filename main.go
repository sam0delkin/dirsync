package main

import (
	"fmt"
	"github.com/alexflint/go-arg"
	"github.com/dietsche/rfsnotify"
	"github.com/gookit/goutil/arrutil"
	"github.com/opcoder0/fanotify"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ChangeType int

const (
	CreateChangeType ChangeType = 1
	ModifyChangeType ChangeType = 2
	RemoveChangeType ChangeType = 3
	InitChangeType   ChangeType = 4
)

var args struct {
	Alpha        string   `arg:"env:ALPHA,positional,required" help:"Source sync directory"`
	Beta         string   `arg:"env:BETA,positional,required" help:"Target sync directory"`
	Exclude      []string `arg:"-e,env:EXCLUDES" help:"Exclude paths from sync and watch"`
	PollEnabled  bool     `arg:"-p,env:POLL_ENABLED" help:"Enable polling for new changes"`
	PollInterval int      `arg:"-i,env:POLL_INTERVAL" default:"1" help:"Polling interval (seconds)"`
	SyncInterval int      `arg:"-s,env:SYNC_INTERVAL" default:"100" help:"Sync changes interval (milliseconds)"`
	LogLevel     string   `arg:"-l,env:LOG_LEVEL" default:"error" help:"Logging level [error|info|debug]"`
	AlphaOwner   string   `arg:"-A,env:ALPHA_OWNER" default:"root" help:"Owner of the files in source sync directory"`
	BetaOwner    string   `arg:"-B,env:BETA_OWNER" default:"root" help:"Owner of the files in target sync directory"`
	WatcherType  string   `arg:"-w,env:WATCHER_TYPE" default:"fsnotify" help:"Watcher type [fsnotify|fanotify]"`
	WatchAlpha   bool     `arg:"env:WATCH_ALPHA" help:"Enable watcher for source sync directory"`
	WatchBeta    bool     `arg:"env:WATCH_BETA" help:"Enable watcher for target sync directory"`
	InitDir      bool     `arg:"env:INIT_DIR" help:"Init target directory from source on start"`
}

var synchronizer *Synchronizer

func remove(s []string, i int) []string {
	s[i] = s[len(s)-1]
	return s[:len(s)-1]
}

func pool() {
	var mutex = sync.Mutex{}
	var alphaPaths = []string{}
	var betaPaths = []string{}
	var alphaHash = ""
	var betaHash = ""
	log.Debugln("Starting polling")
	ticker := time.NewTicker(time.Duration(args.PollInterval) * time.Second)
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				mutex.Lock()
				log.Debugln("Poll Routine Start")
				log.Debugln("Checking for changes")
				var newAlphaHash = getPathHash(args.Alpha)
				var newBetaHash = getPathHash(args.Beta)

				if newAlphaHash != alphaHash {
					log.Debugln("Alpha hash changed", alphaHash, newAlphaHash)
				}

				if newBetaHash != betaHash {
					log.Debugln("Alpha hash changed", alphaHash, newAlphaHash)
				}

				if newAlphaHash != alphaHash || newBetaHash != betaHash {

					alphaHash = newAlphaHash
					betaHash = newBetaHash

					alphaPaths = getPathsInfo(args.Alpha, true)
					betaPaths = getPathsInfo(args.Beta, true)

					diffA := arrutil.Differences(alphaPaths, betaPaths, func(a, b any) int {
						return strings.Compare(a.(string), b.(string))
					})
					diffB := arrutil.Differences(betaPaths, alphaPaths, func(a, b any) int {
						return strings.Compare(a.(string), b.(string))
					})

					var diff = arrutil.Unique(append(diffA, diffB...))

					for _, el1 := range diff {
						for i, el2 := range diff {
							if el1 != el2 && 0 == strings.Index(el1, el2) {
								remove(diff, i)
							}
						}
					}

					log.Debugln("Found following diffs", diff)

					for _, s := range diff {
						var p = strings.Split(s, " ")[0]

						if isIgnored(p, false) {
							continue
						}

						if "" == p {
							continue
						}

						var alphaPath = args.Alpha + p
						var betaPath = args.Beta + p
						var alphaExists, betaExists bool

						if _, err := os.Stat(alphaPath); err == nil {
							alphaExists = true
						} else {
							alphaExists = false
						}

						if _, err := os.Stat(betaPath); err == nil {
							betaExists = true
						} else {
							betaExists = false
						}

						if (alphaExists && !betaExists) || (betaExists && !alphaExists) {
							synchronizer.ProcessEvent(RemoveChangeType, p)
						} else {
							synchronizer.ProcessEvent(RemoveChangeType, p)
						}
					}
				} else {
					log.Debugln("Hashes not changed")
				}

				log.Debugln("Poll Routine End")
				mutex.Unlock()
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()
}

func initFsNotifyWatcher() {
	// Create new watcher.
	watcher, err := rfsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	// Start listening for events.
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if synchronizer.initCompleted {
					log.Debugln("[fsnotify] event:", event)
					log.Debugln("[fsnotify] event:", event.Op.String())
					var path = strings.Replace(strings.Replace(event.Name, args.Alpha, "", 1), args.Beta, "", 1)

					if strings.Index(event.Op.String(), "CREATE") >= 0 {
						synchronizer.ProcessEvent(CreateChangeType, path)
					}

					if strings.Index(event.Op.String(), "WRITE") >= 0 {
						synchronizer.ProcessEvent(ModifyChangeType, path)
					}

					if strings.Index(event.Op.String(), "REMOVE") >= 0 {
						synchronizer.ProcessEvent(RemoveChangeType, path)
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Errorln("[fsnotify] error:", err)
			}
		}
	}()

	if args.WatchAlpha {
		err = watcher.AddRecursive(args.Alpha)
	}
	if args.WatchBeta {
		err = watcher.AddRecursive(args.Beta)
	}
	if err != nil {
		log.Fatal(err)
	}

	for _, exclude := range args.Exclude {
		if args.WatchAlpha {
			log.Debugln("[fsnotify] Removing watch: ", args.Alpha+"/"+exclude)
			if _, err := os.Stat(args.Alpha + "/" + exclude); err == nil {
				err = watcher.RemoveRecursive(args.Alpha + "/" + exclude)
			}
		}

		if args.WatchBeta {
			log.Debugln("[fsnotify] Removing watch: ", args.Beta+"/"+exclude)
			if _, err := os.Stat(args.Beta + "/" + exclude); err == nil {
				err = watcher.RemoveRecursive(args.Beta + "/" + exclude)
			}
		}
	}

	if err != nil {
		log.Fatal(err)
	}

	// Block goroutine forever.
	<-make(chan struct{})
}

func initFaNotifyWatcher() {
	if args.WatchAlpha {
		alphaListener, err := fanotify.NewListener(args.Alpha, false, fanotify.PermissionNone)
		if err != nil {
			log.Fatal("[fanotify] Can't init:", err)
			os.Exit(1)
		}
		log.Infoln("[fanotify] Listening to events for:", args.Alpha)
		var eventTypes fanotify.EventType
		eventTypes = fanotify.FileModified |
			fanotify.FileCreated |
			fanotify.FileOrDirectoryCreated |
			fanotify.FileDeleted |
			fanotify.FileOrDirectoryDeleted
		log.Debugln("[fanotify] Listening on the event mask %x\n", uint64(eventTypes))
		err = alphaListener.AddWatch(args.Alpha, eventTypes)
		if err != nil {
			log.Fatal("[fanotify] WatchMount:", err)
			os.Exit(1)
		}
		go alphaListener.Start()
		for event := range alphaListener.Events {
			fmt.Println(event)
			unix.Close(event.Fd)
		}
		alphaListener.Stop()
	}

	if args.WatchBeta {
		alphaListener, err := fanotify.NewListener(args.Beta, false, fanotify.PermissionNone)
		if err != nil {
			log.Fatal("[fanotify] Can't init:", err)
			os.Exit(1)
		}
		log.Infoln("[fanotify] Listening to events for:", args.Beta)
		var eventTypes fanotify.EventType
		eventTypes = fanotify.FileModified |
			fanotify.FileCreated |
			fanotify.FileOrDirectoryCreated |
			fanotify.FileDeleted |
			fanotify.FileOrDirectoryDeleted
		log.Debugln("[fanotify] Listening on the event mask %x\n", uint64(eventTypes))
		err = alphaListener.AddWatch(args.Beta, eventTypes)
		if err != nil {
			log.Fatal("[fanotify] WatchMount:", err)
			os.Exit(1)
		}
		go alphaListener.Start()
		for event := range alphaListener.Events {
			fmt.Println(event)
			unix.Close(event.Fd)
		}
		alphaListener.Stop()
	}
}

func main() {
	arg.MustParse(&args)

	switch args.LogLevel {
	case "info":
		log.SetLevel(log.InfoLevel)
	case "error":
		log.SetLevel(log.ErrorLevel)
	case "debug":
		log.SetLevel(log.DebugLevel)
	}

	args.Alpha, _ = filepath.Abs(args.Alpha)
	args.Beta, _ = filepath.Abs(args.Beta)

	log.Infoln(fmt.Sprintf("Starting sync from %s to %s With the following parameters: %#v", args.Alpha, args.Beta, args))

	synchronizer = NewSynchronizer()

	if args.PollEnabled {
		pool()
	}

	if args.WatcherType == "fsnotify" {
		initFsNotifyWatcher()
	} else if args.WatcherType == "fanotify" {
		initFaNotifyWatcher()
	}

	log.Debugln("Initialization completed")
}
