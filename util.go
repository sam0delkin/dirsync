package main

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"os"
	"os/exec"
	"sort"
	"strings"
)

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
