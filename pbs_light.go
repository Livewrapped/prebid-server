package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/prebid/prebid-server/config"
	pbc "github.com/prebid/prebid-server/prebid_cache_client"
	"github.com/prebid/prebid-server/router"
	"github.com/prebid/prebid-server/server"
	"golang.org/x/sys/windows/svc"

	"github.com/golang/glog"
	"github.com/spf13/viper"

	"github.com/kardianos/osext"
)

// Holds binary revision string
// Set manually at build time using:
//    go build -ldflags "-X main.Rev=`git rev-parse --short HEAD`"
// Populated automatically at build / release time via .travis.yml
//   `gox -os="linux" -arch="386" -output="{{.Dir}}_{{.OS}}_{{.Arch}}" -ldflags "-X main.Rev=`git rev-parse --short HEAD`" -verbose ./...;`
// See issue #559
var Rev string

func init() {
	rand.Seed(time.Now().UnixNano())
	flag.Parse() // read glog settings from cmd line
}

func main() {
	const svcName = "prebid-server"
	path, err := osext.ExecutableFolder()
	glog.Info(path)
	os.Chdir(path)
	isIntSess, err := svc.IsAnInteractiveSession()
	if err != nil {
		glog.Fatalf("failed to determine if we are running in an interactive session: %v", err)
	}

	if !isIntSess {
		runService(svcName)
		return
	}

	exec()
}

func exec() {
	v := viper.New()
	config.SetupViper(v, "pbs")
	cfg, err := config.New(v)
	if err != nil {
		glog.Fatalf("Configuration could not be loaded or did not pass validation: %v", err)
	}
	if err := serve(Rev, cfg); err != nil {
		glog.Errorf("prebid-server failed: %v", err)
	}
}

func serve(revision string, cfg *config.Configuration) error {
	r, err := router.New(cfg)
	if err != nil {
		return err
	}
	// Init prebid cache
	pbc.InitPrebidCache(cfg.CacheURL.GetBaseURL())
	// Add cors support
	corsRouter := router.SupportCORS(r)
	server.Listen(cfg, router.NoCache{Handler: corsRouter}, router.Admin(revision), r.MetricsEngine)
	r.Shutdown()
	return nil
}

type myservice struct{}

func (m *myservice) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
	exec()
loop:
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Stop, svc.Shutdown:
				break loop
			}
		}
	}
	changes <- svc.Status{State: svc.StopPending}
	return
}

func runService(name string) {
	var err error

	run := svc.Run

	err = run(name, &myservice{})
	if err != nil {
		glog.Error(fmt.Sprintf("%s service failed: %v", name, err))
		return
	}
	glog.Info(fmt.Sprintf("%s service stopped", name))
}
