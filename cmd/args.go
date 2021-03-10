package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cyverse/go-irodsclient/client"
	"github.com/cyverse/irodsfs/pkg/irodsfs"
	"golang.org/x/crypto/ssh/terminal"
	"gopkg.in/yaml.v2"

	log "github.com/sirupsen/logrus"
)

const (
	ChildProcessArgument = "child_process"
	iRODSProtocol        = "irods://"
)

// IRODSAccessURL ...
type IRODSAccessURL struct {
	User     string
	Password string
	Host     string
	Port     int
	Zone     string
	Path     string
}

func parseIRODSURL(inputURL string) (*IRODSAccessURL, error) {
	logger := log.WithFields(log.Fields{
		"package":  "main",
		"function": "parseIRODSURL",
	})

	u, err := url.Parse(inputURL)
	if err != nil {
		logger.WithError(err).Error("Error occurred while parsing source URL")
		return nil, err
	}

	user := ""
	password := ""

	if u.User != nil {
		uname := u.User.Username()
		if len(uname) > 0 {
			user = uname
		}

		if pwd, ok := u.User.Password(); ok {
			password = pwd
		}
	}

	host := ""
	host = u.Hostname()

	port := 1247
	if len(u.Port()) > 0 {
		port64, err := strconv.ParseInt(u.Port(), 10, 32)
		if err != nil {
			logger.WithError(err).Error("Error occurred while parsing source URL's port number")
			return nil, err
		}
		port = int(port64)
	}

	fullpath := path.Clean(u.Path)
	zone := ""
	irodsPath := "/"
	if len(fullpath) == 0 || fullpath[0] != '/' {
		err = fmt.Errorf("path (%s) must contain an absolute path", u.Path)
		logger.Error(err)
		return nil, err
	}

	pos := strings.Index(fullpath[1:], "/")
	if pos > 0 {
		zone = strings.Trim(fullpath[1:pos+1], "/")
		irodsPath = fullpath // starts with zone
	} else if pos == -1 {
		// no path
		zone = strings.Trim(fullpath[1:], "/")
		irodsPath = fullpath
	}

	if len(zone) == 0 || len(irodsPath) == 0 {
		err = fmt.Errorf("path (%s) must contain an absolute path", inputURL)
		logger.Error(err)
		return nil, err
	}

	return &IRODSAccessURL{
		User:     user,
		Password: password,
		Host:     host,
		Port:     port,
		Zone:     zone,
		Path:     irodsPath,
	}, nil
}

func inputMissingParams(config *irodsfs.Config) error {
	logger := log.WithFields(log.Fields{
		"package":  "main",
		"function": "inputMissingParams",
	})

	if len(config.ProxyUser) == 0 {
		fmt.Print("Username: ")
		fmt.Scanln(&config.ProxyUser)

		config.ClientUser = config.ProxyUser
	}

	if len(config.Password) == 0 {
		fmt.Print("Password: ")
		bytePassword, err := terminal.ReadPassword(int(syscall.Stdin))
		fmt.Print("\n")
		if err != nil {
			logger.WithError(err).Error("Error occurred while reading password")
			return err
		}

		config.Password = string(bytePassword)
	}

	return nil
}

func processArguments() (*irodsfs.Config, error) {
	logger := log.WithFields(log.Fields{
		"package":  "main",
		"function": "processArguments",
	})

	var version bool
	var help bool
	var operationTimeout string
	var connectionIdleTimeout string
	var cacheTimeout string
	var cacheCleanupTime string

	config := irodsfs.NewDefaultConfig()

	// Parse parameters
	flag.BoolVar(&version, "version", false, "Print client version information")
	flag.BoolVar(&version, "v", false, "Print client version information (shorthand form)")
	flag.BoolVar(&help, "h", false, "Print help")
	flag.BoolVar(&config.Foreground, "f", false, "Run in foreground")
	flag.BoolVar(&config.ChildProcess, ChildProcessArgument, false, "")
	flag.StringVar(&config.ProxyUser, "proxyuser", "", "Set iRODS proxy user")
	flag.StringVar(&config.ClientUser, "clientuser", "", "Set iRODS client user")
	flag.StringVar(&config.ProxyUser, "user", "", "Set iRODS user")
	flag.StringVar(&config.ProxyUser, "u", "", "Set iRODS user (shorthand form)")
	flag.StringVar(&config.Password, "password", "", "Set iRODS client password")
	flag.StringVar(&config.Password, "p", "", "Set iRODS client password (shorthand form)")
	flag.IntVar(&config.BlockSize, "blocksize", irodsfs.BlockSizeDefault, "Set data transfer block size")
	flag.IntVar(&config.ReadAheadMax, "readahead", irodsfs.ReadAheadMaxDefault, "Set read-ahead size")
	flag.IntVar(&config.ConnectionMax, "connectionmax", irodsfs.ConnectionMaxDefault, "Set max data transfer connections")
	flag.StringVar(&operationTimeout, "operationtimeout", "", "Set filesystem operation timeout")
	flag.StringVar(&connectionIdleTimeout, "connectionidletimeout", "", "Set idle data transfer timeout")
	flag.StringVar(&cacheTimeout, "cachetimeout", "", "Set filesystem cache timeout")
	flag.StringVar(&cacheCleanupTime, "cachecleanuptime", "", "Set filesystem cache cleanup time")

	flag.Parse()

	if version {
		info, err := client.GetVersionJSON()
		if err != nil {
			logger.WithError(err).Error("Could not get client version info")
			return nil, err
		}

		fmt.Println(info)
		os.Exit(0)
	}

	if help {
		flag.Usage()
		os.Exit(0)
	}

	if flag.NArg() != 2 {
		flag.Usage()
		err := fmt.Errorf("Illegal arguments given, required 2, but received %d", flag.NArg())
		logger.Error(err)
		return nil, err
	}

	// time
	if len(operationTimeout) > 0 {
		timeout, err := time.ParseDuration(operationTimeout)
		if err != nil {
			logger.WithError(err).Error("Could not parse Operation Timeout parameter into time.duration")
			return nil, err
		}

		config.OperationTimeout = timeout
	}

	if len(connectionIdleTimeout) > 0 {
		timeout, err := time.ParseDuration(connectionIdleTimeout)
		if err != nil {
			logger.WithError(err).Error("Could not parse Connection Idle Timeout parameter into time.duration")
			return nil, err
		}

		config.ConnectionIdleTimeout = timeout
	}

	if len(cacheTimeout) > 0 {
		timeout, err := time.ParseDuration(cacheTimeout)
		if err != nil {
			logger.WithError(err).Error("Could not parse Cache Timeout parameter into time.duration")
			return nil, err
		}

		config.CacheTimeout = timeout
	}

	if len(cacheCleanupTime) > 0 {
		timeout, err := time.ParseDuration(cacheCleanupTime)
		if err != nil {
			logger.WithError(err).Error("Could not parse Cache Cleanup Time parameter into time.duration")
			return nil, err
		}

		config.CacheCleanupTime = timeout
	}

	// the first argument contains irods://HOST:PORT/ZONE/inputPath...
	inputPath := flag.Arg(0)
	if strings.HasPrefix(inputPath, iRODSProtocol) {
		// inputPath can be a single iRODS collection stating with irods://,
		access, err := parseIRODSURL(inputPath)
		if err != nil {
			logger.WithError(err).Error("Could not parse iRODS source path")
			return nil, err
		}

		if len(access.Host) > 0 {
			config.Host = access.Host
		}

		if access.Port > 0 {
			config.Port = access.Port
		}

		if len(access.User) > 0 {
			config.ProxyUser = access.User
		}

		if len(access.Password) > 0 {
			config.Password = access.Password
		}

		if len(access.Zone) > 0 {
			config.Zone = access.Zone
		}

		if len(access.Path) > 0 {
			config.PathMappings = []irodsfs.PathMapping{
				irodsfs.NewPathMappingForDir(access.Path, "/"),
			}
		}

		if len(config.ClientUser) == 0 {
			config.ClientUser = config.ProxyUser
		}
	} else if strings.HasSuffix(inputPath, ".yaml") || strings.HasSuffix(inputPath, ".yml") {
		// inputPath can be a local file
		inputAbsPath, err := filepath.Abs(inputPath)
		if err != nil {
			logger.WithError(err).Errorf("Could not access the local yaml file %s", inputPath)
			return nil, err
		}

		fileinfo, err := os.Stat(inputAbsPath)
		if err != nil {
			logger.WithError(err).Errorf("local yaml file (%s) error", inputAbsPath)
			return nil, err
		}

		if fileinfo.IsDir() {
			logger.WithError(err).Errorf("local yaml file (%s) is not a file", inputAbsPath)
			return nil, fmt.Errorf("local yaml file (%s) is not a file", inputAbsPath)
		}

		yamlBytes, err := ioutil.ReadFile(inputAbsPath)
		if err != nil {
			logger.WithError(err).Errorf("Could not read the local yaml file %s", inputAbsPath)
			return nil, err
		}

		pathMappings := []irodsfs.PathMapping{}
		err = yaml.Unmarshal(yamlBytes, &pathMappings)
		if err != nil {
			return nil, fmt.Errorf("YAML Unmarshal Error - %v", err)
		}

		config.PathMappings = pathMappings
	} else {
		//
		err := fmt.Errorf("Source path must be an iRODS URL ('irods://host:port/zone/path/to/the/collection') or a local path ('/home/user/path/to/the/mapping_file.yaml')")
		logger.Error(err)
		return nil, err
	}

	err := inputMissingParams(config)
	if err != nil {
		logger.WithError(err).Error("Could not input missing parameters")
		return nil, err
	}

	// the second argument is local directory that irodsfs will be mounted
	mountpoint, err := filepath.Abs(flag.Arg(1))
	if err != nil {
		logger.WithError(err).Errorf("Could not access the mount point %s", flag.Arg(1))
		return nil, err
	}

	config.MountPath = mountpoint

	return config, nil
}
