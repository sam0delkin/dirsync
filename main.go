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
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var initCompleted = false
var eventsEnabled = true

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
	PollInterval int      `arg:"-i,env:POLL_INTERVAL" default:"1" help:"Polling interval"`
	LogLevel     string   `arg:"-l,env:LOG_LEVEL" default:"error" help:"Logging level [error|info|debug]"`
	AlphaOwner   string   `arg:"-A,env:ALPHA_OWNER" default:"root" help:"Owner of the files in source sync directory"`
	BetaOwner    string   `arg:"-B,env:BETA_OWNER" default:"root" help:"Owner of the files in target sync directory"`
	WatcherType  string   `arg:"-w,env:WATCHER_TYPE" default:"fsnotify" help:"Watcher type [fsnotify|fanotify]"`
	WatchAlpha   bool     `arg:"env:WATCH_ALPHA" help:"Enable watcher for source sync directory"`
	WatchBeta    bool     `arg:"env:WATCH_BETA" help:"Enable watcher for target sync directory"`
	InitDir      bool     `arg:"env:INIT_DIR" help:"Init target directory from source on start"`
}

func isIgnored(path string, logging bool) bool {
	for _, exclude := range args.Exclude {
		if (0 == strings.Index(path, exclude)) || (0 == strings.Index(path, "/"+exclude)) {
			if logging {
				log.Infoln("Ignoring path: ", path)
			}

			return true
		}
	}

	return false
}

func processChange(changeType ChangeType, path string) {
	if isIgnored(path, true) {
		return
	}

	var mutex = sync.Mutex{}
	var alphaPath = args.Alpha + path
	var betaPath = args.Beta + path
	var alphaExists, betaExists bool

	mutex.Lock()

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
	switch changeType {
	case InitChangeType:
		// alpha always wins
		if !betaExists {
			log.Infoln("[INIT] Copying ", alphaPath, betaPath)
			command := "rm -rf " + betaPath + " && cp -rf -p " + alphaPath + " " + betaPath + " && chown -R " + args.BetaOwner + " " + betaPath
			log.Debugln("[INIT] Command ", command)
			_, err := exec.Command("sh", "-c", command).Output()
			if err != nil {
				log.Errorln("Failed to copy file from alpha to beta: ", command, err)
			}
		} else if !alphaExists {
			log.Infoln("[INIT] Removing ", betaPath)
			command := "rm -rf " + betaPath
			log.Debugln("[INIT] Command ", command)
			_, err := exec.Command("sh", "-c", command).Output()
			if err != nil {
				log.Errorln("Failed to remove file from beta: ", command, err)
			}
		} else {
			log.Infoln("[INIT] Copying ", alphaPath, betaPath)
			command := "rm -rf " + betaPath + " && cp  -rf -p " + alphaPath + " " + betaPath + " && chown -R " + args.BetaOwner + " " + betaPath
			log.Debugln("[INIT] Command ", command)
			_, err := exec.Command("sh", "-c", command).Output()
			if err != nil {
				log.Errorln("Failed to copy file from alpha to beta: ", command, err)
			}
		}
	case CreateChangeType:
		if !betaExists {
			info, err := os.Stat(alphaPath)
			if err != nil {
				log.Errorln("Failed to get file stat: ", err)
			} else {
				log.Infoln("[CREATE] Copying ", alphaPath, betaPath)
				if info.IsDir() {
					command := "mkdir --parents " + betaPath
					log.Debugln("[CREATE] Command ", command)
					_, err := exec.Command("sh", "-c", command).Output()
					if err != nil {
						log.Errorln("Failed to copy file from alpha to beta: ", command, err)
					}
				} else {
					command := "cp -f -p " + alphaPath + " " + betaPath + " && chown -R " + args.BetaOwner + " " + betaPath
					log.Debugln("[CREATE] Command ", command)
					_, err = exec.Command("sh", "-c", command).Output()
					if err != nil {
						log.Errorln("Failed to copy file from alpha to beta: ", command, err)
					}
				}
			}
		} else if !alphaExists {
			info, err := os.Stat(betaPath)
			if err != nil {
				log.Debugln("Failed to get file stat: ", err)
			} else {
				log.Infoln("[CREATE] Copying ", betaPath, alphaPath)
				if info.IsDir() {
					command := "mkdir --parents " + alphaPath
					log.Debugln("[CREATE] Command ", command)
					_, err := exec.Command("sh", "-c", command).Output()
					if err != nil {
						log.Errorln("Failed to copy file from alpha to beta: ", command, err)
					}
				} else {
					command := "cp -f -p " + betaPath + " " + alphaPath + " && chown -R " + args.AlphaOwner + " " + alphaPath
					log.Debugln("[CREATE] Command ", command)
					_, err = exec.Command("sh", "-c", command).Output()
					if err != nil {
						log.Errorln("Failed to copy file from alpha to beta: ", command, err)
					}
				}
			}
		}
	case ModifyChangeType:
		alphaInfo, err := os.Stat(alphaPath)
		if err != nil {
			log.Errorln("Failed to get file stat: ", err)

			return
		}
		betaInfo, err := os.Stat(betaPath)
		if err != nil {
			log.Debugln("Failed to get file stat: ", err)

			return
		}

		if alphaInfo.IsDir() || betaInfo.IsDir() {
			command := "touch " + betaPath + " && touch " + alphaPath
			log.Debugln("[MODIFY] Command ", command)
			_, err := exec.Command("sh", "-c", command).Output()
			if err != nil {
				log.Errorln("Failed to modify mtime of dir: ", command, err)

				return
			}

			return
		}

		if alphaInfo.ModTime().UnixMicro() > betaInfo.ModTime().UnixMicro() {
			log.Infoln("[MODIFY] Modifying from source to target ", alphaPath, betaPath)
			command := "cp -f -p " + alphaPath + " " + betaPath + " && chown -R " + args.BetaOwner + " " + betaPath
			log.Debugln("[MODIFY] Command ", command)
			_, err = exec.Command("sh", "-c", command).Output()
			if err != nil {
				log.Errorln("Failed to copy file from alpha to beta: ", command, err)
			}
		} else if alphaInfo.ModTime().UnixMicro() < betaInfo.ModTime().UnixMicro() {
			log.Infoln("[MODIFY] Modifying from source to target ", alphaPath, betaPath)
			command := "cp -f -p " + betaPath + " " + alphaPath + " && chown -R " + args.AlphaOwner + " " + alphaPath
			log.Debugln("[MODIFY] Command ", command)
			_, err = exec.Command("sh", "-c", command).Output()
			if err != nil {
				log.Errorln("Failed to copy file from alpha to beta: ", command, err)
			}
		}
	case RemoveChangeType:
		if alphaExists {
			log.Debugln("[REMOVE] Alpha exists", alphaPath)
		} else {
			log.Debugln("[REMOVE] Alpha not exists", alphaPath)
		}
		if betaExists {
			log.Debugln("[REMOVE] Beta exists", betaPath)
		} else {
			log.Debugln("[REMOVE] Beta not exists", betaPath)
		}

		if !betaExists && alphaExists {
			log.Infoln("[REMOVE] Removing ", alphaPath)
			command := "rm -rf " + alphaPath
			log.Debugln("[REMOVE] Command ", command)
			_, err := exec.Command("sh", "-c", command).Output()
			if err != nil {
				log.Errorln("Failed to remove file from alpha to beta: ", command, err)
			}
		} else if !alphaExists && betaExists {
			log.Infoln("[REMOVE] Removing ", betaPath)
			command := "rm -rf " + betaPath
			log.Debugln("[REMOVE] Command ", command)
			_, err := exec.Command("sh", "-c", command).Output()
			if err != nil {
				log.Errorln("Failed to remove file: ", command, err)
			}
		}
	}

	mutex.Unlock()
}

func getPathsInfo(path string, includeMtime bool) []string {
	var paths = []string{}
	var ignores = ""

	for _, exclude := range args.Exclude {
		ignores += fmt.Sprintf(" --exclude \"%s/%s*\" ", path, exclude)
	}
	var command = fmt.Sprintf("cd %s && du -a %s | awk '{print $2}' | xargs realpath | xargs  stat -c \"%%n %%Y\" 2>&1 || true", path, ignores)
	log.Debugln("[PATH_INFO] Command", command)
	out, err := exec.Command("sh", "-c", command).Output()
	if err != nil {
		log.Fatal("Can't execute ls. Exiting. Error: ", err)
		os.Exit(1)
	}
	var outStrings = strings.Split(string(out), "\n")
	for _, s := range outStrings {
		var pathParts = strings.Split(s, " ")

		if len(pathParts) < 2 {
			continue
		}

		pathParts[0] = strings.Replace(pathParts[0], path, "", 1)
		var newPathParts []string
		if includeMtime {
			newPathParts = []string{pathParts[0], pathParts[1]}
		} else {
			newPathParts = []string{pathParts[0]}
		}
		s = strings.Join(newPathParts, " ")
		paths = append(paths, s)
	}

	sort.Strings(paths)

	return paths
}

func getPathHash(path string) string {
	var ignores = ""

	for _, exclude := range args.Exclude {
		ignores += fmt.Sprintf(" --exclude \"%s/%s*\" ", path, exclude)
	}
	var command = fmt.Sprintf("(cd %s && du -a %s | awk '{print $2}' | xargs realpath | xargs stat -c \"%%n %%Y\" 2>&1 || true) | sha256sum", path, ignores)
	log.Debugln("[PATH_INFO] Command", command)
	out, err := exec.Command("sh", "-c", command).Output()
	if err != nil {
		log.Fatal("Can't execute ls. Exiting. Error: ", err)
		os.Exit(1)
	}

	return string(out)
}

func initDir() {
	log.Debugln("First run. Checking whole directory structure")
	var alphaPaths = getPathsInfo(args.Alpha, true)
	var betaPaths = getPathsInfo(args.Beta, true)
	var diff []string

	if len(betaPaths) > 1 {
		diffA := arrutil.Differences(alphaPaths, betaPaths, func(a, b any) int {
			return strings.Compare(a.(string), b.(string))
		})
		diffB := arrutil.Differences(betaPaths, alphaPaths, func(a, b any) int {
			return strings.Compare(a.(string), b.(string))
		})

		diff = arrutil.Unique(append(diffA, diffB...))
	} else {
		diff = alphaPaths
	}

	log.Debugln("Found following diffs", diff)
	var processedPaths []string

	for _, s := range diff {
		path := strings.Split(s, " ")[0]
		var pathProcessed = false

		for _, pp := range processedPaths {
			if strings.Index(path, pp) >= 0 {
				pathProcessed = true
				break
			}
		}

		if isIgnored(path, false) || "" == path || pathProcessed {
			continue
		}

		processedPaths = append(processedPaths, path)

		processChange(InitChangeType, path)
	}

	initCompleted = true
}

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
							processChange(RemoveChangeType, p)
						} else {
							processChange(ModifyChangeType, p)
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
				if initCompleted {
					if eventsEnabled {
						eventsEnabled = false
						log.Debugln("[fsnotify] event:", event)
						log.Debugln("[fsnotify] event:", event.Op.String())
						var path = strings.Replace(strings.Replace(event.Name, args.Alpha, "", 1), args.Beta, "", 1)

						if strings.Index(event.Op.String(), "CREATE") >= 0 {
							processChange(CreateChangeType, path)
						}

						if strings.Index(event.Op.String(), "WRITE") >= 0 {
							processChange(ModifyChangeType, path)
						}

						if strings.Index(event.Op.String(), "REMOVE") >= 0 {
							processChange(RemoveChangeType, path)
						}
						eventsEnabled = true
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

	if args.InitDir {
		initDir()
	} else {
		initCompleted = true
	}

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
