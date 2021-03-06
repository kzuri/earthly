package autocomplete

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strings"

	"github.com/earthly/earthly/earthfile2llb"
)

func hasTargetOrCommand(line string) bool {
	splits := strings.Split(line, " ")
	for i, s := range splits {
		if i == 0 {
			continue // skip earth command
		}
		if len(s) == 0 {
			continue // skip empty commands
		}
		if s[0] == '-' {
			continue // skip flags
		}
		return true // found a command or target
	}
	return false
}

// parseLine parses a bash COMP_LINE and COMP_POINT variables into the argument to expand
// e.g. line="earth --argum", cursorLoc=10; this will return "--ar"
func parseLine(line string, cursorLoc int) string {
	var i int
	for i = cursorLoc; i > 0; i-- {
		if line[i-1] == ' ' {
			break
		}
	}
	if i >= cursorLoc {
		return ""
	}
	return line[i:cursorLoc]
}

func trimFlag(prefix string) (string, bool) {
	if len(prefix) == 1 && prefix[0] == '-' {
		return "", true
	}
	if strings.HasPrefix(prefix, "--") {
		return prefix[2:], true
	}
	return "", false
}

func isLocalPath(path string) bool {
	for _, prefix := range []string{".", "..", "/", "~"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func getPotentialTarget(prefix string) ([]string, error) {
	splits := strings.SplitN(prefix, "+", 2)
	if len(splits) < 2 {
		return []string{}, nil
	}
	dirPath := splits[0]

	realDirPath := dirPath
	if strings.HasPrefix(realDirPath, "~") {
		if strings.HasPrefix(realDirPath, "~/") {
			currentUser, err := user.Current()
			if err != nil {
				return nil, err
			}
			realDirPath = strings.Replace(realDirPath, "~", currentUser.HomeDir, 1)
		} else {
			username := strings.Replace(strings.Split(prefix, "/")[0], "~", "", 1)
			currentUser, err := user.Lookup(username)
			if err != nil {
				return nil, err
			}
			realDirPath = strings.Replace(realDirPath, fmt.Sprintf("~%s", username), currentUser.HomeDir, 1)
		}
	}

	targets, err := earthfile2llb.GetTargets(path.Join(realDirPath, "Earthfile"))
	if err != nil {
		return nil, err
	}

	potentials := []string{}
	for _, target := range targets {
		s := dirPath + "+" + target + " "
		if strings.HasPrefix(s, prefix) {
			potentials = append(potentials, s)
		}
	}

	return potentials, nil
}

func getUser(username string) (*user.User, error) {
	username = strings.Replace(username, "~", "", 1)
	return user.Lookup(username)
}

func getUsers() ([]string, error) {
	u, err := user.Current()
	if err != nil {
		return nil, err
	}

	home := filepath.Dir(u.HomeDir)
	directoryList, err := ioutil.ReadDir(home)
	if err != nil {
		return nil, err
	}
	var res []string
	for _, s := range directoryList {
		if !s.IsDir() {
			continue
		}
		u, err := user.Lookup(s.Name())
		if err != nil {
			continue
		}
		res = append(res, fmt.Sprintf("~%s", u.Username))
	}
	return res, nil
}

func matchAndFilterUser(users []string, prefix string) (match bool, filter []string) {
	for _, s := range users {
		if s == prefix {
			match = true
			return
		}
		if strings.HasPrefix(s, prefix) {
			filter = append(filter, s)
		}
	}
	return
}

func resolveUserPath(prefix string) ([]string, error) {
	users, err := getUsers()
	if err != nil {
		return nil, err
	}

	if prefix == "~" {
		return users, nil
	}

	replacer := ""
	username := ""
	if strings.HasPrefix(prefix, "~/") {
		replacer = "~"
		u, err := user.Current()
		if err != nil {
			return nil, err
		}
		username = fmt.Sprintf("~%s", u.Username)
	} else {
		// match and filter
		sl := strings.Split(prefix, "/")
		username = sl[0]
		replacer = username
		match, filters := matchAndFilterUser(users, username)
		// filter users by prefix
		if !match {
			return filters, nil
		}
	}

	u, err := getUser(username)
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(prefix, "~/") {
		prefix = strings.Replace(prefix, "~", u.HomeDir, 1)
	} else {
		prefix = strings.Replace(prefix, username, u.HomeDir, 1)
	}
	var result []string
	if hasEarthfile(prefix) {
		x := strings.Replace(prefix, u.HomeDir, replacer, 1)
		result = append(result, fmt.Sprintf("%s+", x))
	}

	paths, err := lsPath(prefix)
	if err != nil {
		return nil, err
	}

	for _, p := range paths {
		result = append(result, strings.Replace(p, u.HomeDir, replacer, 1))
	}
	return result, nil
}

func filterPath(p, filter string) ([]string, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, errors.New("not a directory")
	}

	lDir, err := listDirectory(p)
	if err != nil {
		return nil, err
	}
	var res []string
	for _, s := range lDir {
		if strings.HasPrefix(s, path.Join(p, filter)) {
			res = append(res, s)
		}
	}
	return res, nil
}

func listDirectory(prefix string) ([]string, error) {
	files, err := ioutil.ReadDir(prefix)
	if err != nil {
		return nil, err
	}
	var paths []string

	for _, file := range files {
		s := path.Join(prefix, file.Name())
		if hasEarthfile(s) {
			paths = append(paths, s+"+")
		}
		if hasSubDirs(s) {
			paths = append(paths, s+"/")
		}
	}
	return paths, nil
}

func lsPath(prefix string) ([]string, error) {
	fi, err := os.Stat(prefix)
	if err != nil {
		d, f := path.Split(prefix)
		return filterPath(d, f)
	}

	switch mode := fi.Mode(); {
	case mode.IsDir():
		return listDirectory(prefix)
	case mode.IsRegular():
		// do file stuff
		return nil, errors.New("it is not a directory")
	default:
		return nil, errors.New("it is not a directory")
	}
}

func getPotentialPaths(prefix string) ([]string, error) {
	if prefix == "." {
		return []string{"./", "../"}, nil
	}

	// users implementation
	if strings.HasPrefix(prefix, "~") {
		return resolveUserPath(prefix)
	}
	// generic folder
	var res []string
	if hasEarthfile(prefix) {
		res = append(res, fmt.Sprintf("%s+", prefix))
	}
	l, _ := lsPath(prefix)
	return append(res, l...), nil
}

// GetPotentials returns a list of potential arguments for shell auto completion
func GetPotentials(compLine string, compPoint int, flags, commands []string) ([]string, error) {
	potentials := []string{}

	prefix := parseLine(compLine, compPoint)

	// already has a full command or target (we're done now)
	if hasTargetOrCommand(compLine) && prefix == "" {
		return potentials, nil
	}

	if flagPrefix, ok := trimFlag(prefix); ok {
		for _, s := range flags {
			if strings.HasPrefix(s, flagPrefix) {
				potentials = append(potentials, "--"+s+" ")
			}
		}
		return potentials, nil
	}

	if isLocalPath(prefix) || strings.HasPrefix(prefix, "+") {
		if strings.Contains(prefix, "+") {
			return getPotentialTarget(prefix)
		}
		return getPotentialPaths(prefix)
	}

	if prefix == "" {
		if hasEarthfile(".") {
			potentials = append(potentials, "+")
		}
		potentials = append(potentials, "./")
	}

	for _, cmd := range commands {
		if strings.HasPrefix(cmd, prefix) {
			potentials = append(potentials, cmd+" ")
		}
	}
	return potentials, nil
}

func hasEarthfile(dirPath string) bool {
	info, err := os.Stat(path.Join(dirPath, "Earthfile"))
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func hasSubDirs(dirPath string) bool {
	files, err := ioutil.ReadDir(dirPath)
	if err != nil {
		return false
	}

	for _, file := range files {
		if file.IsDir() {
			return true
		}
	}
	return false
}
