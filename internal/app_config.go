package internal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml"
)

var DEF_CONFIG_ORDER = []string{
	"/usr/lib/sota/conf.d",
	"/var/sota/sota.toml",
	"/etc/sota/conf.d/",
}

var ErrNoWritableFound = errors.New("no writable TOML file found")

type cfgFile struct {
	name string
	path string

	tree *toml.Tree
}

type AppConfig struct {
	cfgs []*cfgFile
}

// NewAppConfig parses config files as per:
// https://docs.ota.here.com/ota-client/latest/aktualizr-config-options.html#_how_toml_files_are_processed
func NewAppConfig(configPaths []string) (*AppConfig, error) {
	configsMap := make(map[string]string)

	// Build up the files
	for _, path := range configPaths {
		if st, err := os.Stat(path); err == nil {
			if st.IsDir() {
				if entries, err := os.ReadDir(path); err == nil {
					for _, entry := range entries {
						if strings.HasSuffix(entry.Name(), ".toml") {
							configsMap[entry.Name()] = filepath.Join(path, entry.Name())
						}
					}
				}
			} else {
				configsMap[st.Name()] = path
			}
		}
	}

	// Sort them reverse alphabetically (so most significant is first)
	keys := make([]*cfgFile, 0, len(configsMap))
	for k, v := range configsMap {
		keys = append(keys, &cfgFile{name: k, path: v})
	}
	sort.Slice(keys[:], func(i, j int) bool {
		return keys[i].name > keys[j].name
	})

	var err error
	for _, cfg := range keys {
		cfg.tree, err = toml.LoadFile(cfg.path)
		if err != nil {
			return nil, err
		}
	}

	cfg := AppConfig{cfgs: keys}
	return &cfg, nil
}

func (c AppConfig) Get(key string) string {
	for i := range c.cfgs {
		val := c.cfgs[i].tree.GetDefault(key, "").(string)
		if len(val) > 0 {
			return val
		}
	}
	return ""
}

func (c AppConfig) GetOrDie(key string) string {
	val := c.Get(key)
	if len(val) == 0 {
		var paths []string
		for _, cfg := range c.cfgs {
			paths = append(paths, cfg.path)
		}
		fmt.Println("ERROR: Missing", key, "in", strings.Join(paths, ","))
		os.Exit(1)
	}
	return val
}

func (c AppConfig) GetDefault(key string, defval string) string {
	val := c.Get(key)
	if len(val) == 0 {
		val = defval
	}
	return val
}

func isWritable(path string) bool {
	dirName := filepath.Dir(path)
	baseName := filepath.Base(path)

	tmpPath := filepath.Join(dirName, "."+baseName+".tmp")
	fd, err := os.Create(tmpPath)
	if fd != nil {
		_ = fd.Close()
		_ = os.Remove(tmpPath)
	}
	return err == nil
}

// isConfigManaged returns whether or not a file is managed by fioctl/fioconfig
// and thus should not be written to by this.
func isConfigManaged(path string) bool {
	return path == "z-50-fioctl.toml"
}

// findWritableFile looks at the keyVals and determines which toml file
// we should update.
//
// It's unlikely but you could theoretically have keyvals in more than
// one config file. This makes its hard to do atomically. So, we should
// find the most significant config file and put all the keyvals into
// that one.
//
// Returns nil if no writable file is found
func (c AppConfig) findWritableFile(keyVals map[string]string) (*cfgFile, error) {
	for i := range c.cfgs {
		for k := range keyVals {
			if c.cfgs[i].tree.Has(k) {
				if isConfigManaged(c.cfgs[i].name) {
					return nil, fmt.Errorf("unable to override config-managed file: %s", c.cfgs[i].path)
				}

				if !isWritable(c.cfgs[i].path) {
					// Work our way back up more significant files to see if there
					// is one we can update this value from
					for j := i - 1; j >= 0; j-- {
						if !isConfigManaged(c.cfgs[j].name) && isWritable(c.cfgs[j].path) {
							return c.cfgs[j], nil
						}
					}
					return nil, fmt.Errorf("unable to write to file: %s", c.cfgs[i].path)
				}
				return c.cfgs[i], nil
			}
		}
	}
	return nil, ErrNoWritableFound
}
