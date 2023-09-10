package main

import (
	"github.com/gookit/goutil/arrutil"
	"github.com/phf/go-queue/queue"
	log "github.com/sirupsen/logrus"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Event struct {
	Type ChangeType
	Path string
}

type Synchronizer struct {
	mutex         sync.Mutex
	queue         *queue.Queue
	initCompleted bool
}

func NewSynchronizer() *Synchronizer {
	return new(Synchronizer).Init()
}

// Init initializes or clears queue q.
func (q *Synchronizer) Init() *Synchronizer {
	q.mutex = sync.Mutex{}
	q.queue = queue.New()
	q.initCompleted = false
	if args.InitDir {
		q.initDir()
	} else {
		q.initCompleted = true
	}
	q.pool()

	return q
}

func (q *Synchronizer) ProcessEvent(changeType ChangeType, path string) {
	q.queue.PushBack(Event{
		changeType,
		path,
	})
}

func (q *Synchronizer) pool() {
	log.Debugln("[sync] Starting polling")
	ticker := time.NewTicker(time.Duration(args.SyncInterval) * time.Millisecond)
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				q.mutex.Lock()
				log.Debugln("[sync] Starting sync iteration")
				var m =  make(map[string]ChangeType)
				for true {
					var item = q.queue.PopFront()
					var value = m[item.(Event).Path]

					if (value != 0) {
						if (value == RemoveChangeType && item.(Event).Type != RemoveChangeType) {
							q.processChange(item.(Event).Type, item.(Event).Path)
						}

						continue
					}

					if item == nil {
						break
					}

					q.processChange(item.(Event).Type, item.(Event).Path)
				}
				log.Debugln("[sync] All events processed")
				q.mutex.Unlock()
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()
}

func (q *Synchronizer) processChange(changeType ChangeType, path string) {
	if isIgnored(path, true) {
		return
	}

	var alphaPath = args.Alpha + path
	var betaPath = args.Beta + path
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
	switch changeType {
	case InitChangeType:
		// alpha always wins
		if !betaExists {
			log.Infoln("[sync][INIT] Copying ", alphaPath, betaPath)
			command := "rm -rf " + betaPath + " && cp -rf -p " + alphaPath + " " + betaPath + " && chown -R " + args.BetaOwner + " " + betaPath
			log.Debugln("[sync][INIT] Command ", command)
			_, err := exec.Command("sh", "-c", command).Output()
			if err != nil {
				log.Errorln("[sync]Failed to copy file from alpha to beta: ", command, err)
			}
		} else if !alphaExists {
			log.Infoln("[sync][INIT] Removing ", betaPath)
			command := "rm -rf " + betaPath
			log.Debugln("[sync][INIT] Command ", command)
			_, err := exec.Command("sh", "-c", command).Output()
			if err != nil {
				log.Errorln("[sync] Failed to remove file from beta: ", command, err)
			}
		} else {
			log.Infoln("[sync][INIT] Copying ", alphaPath, betaPath)
			command := "rm -rf " + betaPath + " && cp  -rf -p " + alphaPath + " " + betaPath + " && chown -R " + args.BetaOwner + " " + betaPath
			log.Debugln("[sync][INIT] Command ", command)
			_, err := exec.Command("sh", "-c", command).Output()
			if err != nil {
				log.Errorln("[sync] Failed to copy file from alpha to beta: ", command, err)
			}
		}
	case CreateChangeType:
		if !betaExists && args.WatchAlpha {
			info, err := os.Stat(alphaPath)
			if err != nil {
				log.Errorln("[sync] Failed to get file stat: ", err)
			} else {
				log.Infoln("[sync][CREATE] Copying ", alphaPath, betaPath)
				if info.IsDir() {
					command := "mkdir --parents " + betaPath
					log.Debugln("[sync][CREATE] Command ", command)
					_, err := exec.Command("sh", "-c", command).Output()
					if err != nil {
						log.Errorln("[sync] Failed to copy file from alpha to beta: ", command, err)
					}
				} else {
					command := "cp -f -p " + alphaPath + " " + betaPath + " && chown -R " + args.BetaOwner + " " + betaPath
					log.Debugln("[sync][CREATE] Command ", command)
					_, err = exec.Command("sh", "-c", command).Output()
					if err != nil {
						log.Errorln("[sync] Failed to copy file from alpha to beta: ", command, err)
					}
				}
			}
		} else if !alphaExists && args.WatchBeta {
			info, err := os.Stat(betaPath)
			if err != nil {
				log.Debugln("[sync] Failed to get file stat: ", err)
			} else {
				log.Infoln("[sync][CREATE] Copying ", betaPath, alphaPath)
				if info.IsDir() {
					command := "mkdir --parents " + alphaPath
					log.Debugln("[sync][CREATE] Command ", command)
					_, err := exec.Command("sh", "-c", command).Output()
					if err != nil {
						log.Errorln("[sync] Failed to copy file from alpha to beta: ", command, err)
					}
				} else {
					command := "cp -f -p " + betaPath + " " + alphaPath + " && chown -R " + args.AlphaOwner + " " + alphaPath
					log.Debugln("[sync][CREATE] Command ", command)
					_, err = exec.Command("sh", "-c", command).Output()
					if err != nil {
						log.Errorln("[sync] Failed to copy file from alpha to beta: ", command, err)
					}
				}
			}
		}
	case ModifyChangeType:
		alphaInfo, err := os.Stat(alphaPath)
		if err != nil {
			log.Errorln("[sync] Failed to get file stat: ", err)

			return
		}
		betaInfo, err := os.Stat(betaPath)
		if err != nil {
			log.Debugln("[sync] Failed to get file stat: ", err)

			return
		}

		if alphaInfo.IsDir() || betaInfo.IsDir() {
			command := "touch " + betaPath + " && touch " + alphaPath
			log.Debugln("[sync][MODIFY] Command ", command)
			_, err := exec.Command("sh", "-c", command).Output()
			if err != nil {
				log.Errorln("[sync] Failed to modify mtime of dir: ", command, err)

				return
			}

			return
		}

		if alphaInfo.ModTime().UnixMicro() > betaInfo.ModTime().UnixMicro() && args.WatchAlpha {
			log.Infoln("[sync][MODIFY] Modifying from source to target ", alphaPath, betaPath)
			command := "cp -f -p " + alphaPath + " " + betaPath + " && chown -R " + args.BetaOwner + " " + betaPath
			log.Debugln("[sync][MODIFY] Command ", command)
			_, err = exec.Command("sh", "-c", command).Output()
			if err != nil {
				log.Errorln("[sync] Failed to copy file from alpha to beta: ", command, err)
			}
		} else if alphaInfo.ModTime().UnixMicro() < betaInfo.ModTime().UnixMicro() && args.WatchBeta {
			log.Infoln("[sync][MODIFY] Modifying from source to target ", alphaPath, betaPath)
			command := "cp -f -p " + betaPath + " " + alphaPath + " && chown -R " + args.AlphaOwner + " " + alphaPath
			log.Debugln("[sync][MODIFY] Command ", command)
			_, err = exec.Command("sh", "-c", command).Output()
			if err != nil {
				log.Errorln("[sync] Failed to copy file from alpha to beta: ", command, err)
			}
		}
	case RemoveChangeType:
		if alphaExists {
			log.Debugln("[sync][REMOVE] Alpha exists", alphaPath)
		} else {
			log.Debugln("[sync]REMOVE] Alpha not exists", alphaPath)
		}
		if betaExists {
			log.Debugln("[sync][REMOVE] Beta exists", betaPath)
		} else {
			log.Debugln("[sync][REMOVE] Beta not exists", betaPath)
		}

		if !betaExists && alphaExists && args.WatchBeta {
			log.Infoln("[sync][REMOVE] Removing ", alphaPath)
			command := "rm -rf " + alphaPath
			log.Debugln("[sync][REMOVE] Command ", command)
			_, err := exec.Command("sh", "-c", command).Output()
			if err != nil {
				log.Errorln("[sync] Failed to remove file from alpha to beta: ", command, err)
			}
		} else if !alphaExists && betaExists && args.WatchAlpha {
			log.Infoln("[sync][REMOVE] Removing ", betaPath)
			command := "rm -rf " + betaPath
			log.Debugln("[sync][REMOVE] Command ", command)
			_, err := exec.Command("sh", "-c", command).Output()
			if err != nil {
				log.Errorln("[sync] Failed to remove file: ", command, err)
			}
		}
	}
}

func (q *Synchronizer) initDir() {
	log.Debugln("[sync] First run. Checking whole directory structure")
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

	log.Debugln("[sync] Found following diffs", diff)
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

		q.processChange(InitChangeType, path)
	}

	q.initCompleted = true
}
