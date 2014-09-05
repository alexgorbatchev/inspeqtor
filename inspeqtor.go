package inspeqtor

import (
	"errors"
	"inspeqtor/services"
	"inspeqtor/util"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const (
	VERSION = "1.0.0"
)

type Inspeqtor struct {
	RootDir    string
	SocketPath string
	StartedAt  time.Time

	ServiceManagers []services.InitSystem
	Host            *Host
	Services        []Checkable
	GlobalConfig    *ConfigFile
	Socket          net.Listener
	SilenceUntil    time.Time
}

func New(dir string, socketpath string) (*Inspeqtor, error) {
	i := &Inspeqtor{RootDir: dir,
		SocketPath:   socketpath,
		StartedAt:    time.Now(),
		SilenceUntil: time.Now(),
		Host:         &Host{&Entity{name: "localhost"}},
		GlobalConfig: &ConfigFile{Defaults, map[string]*AlertRoute{}}}
	return i, nil
}

var (
	Term os.Signal = syscall.SIGTERM

	SignalHandlers = map[os.Signal]func(*Inspeqtor){
		Term:         exit,
		os.Interrupt: exit,
	}
	Name      string = "Inspeqtor"
	Licensing string = "Licensed under the GNU Public License 3.0"
)

func (i *Inspeqtor) Start() {
	err := i.openSocket(i.SocketPath)
	if err != nil {
		util.Warn("Could not create Unix socket: %s", err.Error())
		exit(i)
	}

	go func() {
		for {
			i.acceptCommand()
		}
	}()

	go i.runLoop()

	// This method never returns
	handleSignals(i)
}

func (i *Inspeqtor) Parse() error {
	i.ServiceManagers = services.Detect()

	config, err := ParseGlobal(i.RootDir)
	if err != nil {
		return err
	}
	util.DebugDebug("Global config: %+v", config)
	i.GlobalConfig = config

	host, services, err := ParseInq(i.GlobalConfig, i.RootDir+"/conf.d")
	if err != nil {
		return err
	}
	i.Host = host
	i.Services = services

	util.DebugDebug("Config: %+v", config)
	util.DebugDebug("Host: %+v", host)
	for _, val := range services {
		util.DebugDebug("Service: %+v", val)
	}
	return nil
}

// private

func (i *Inspeqtor) openSocket(path string) error {
	if i.Socket != nil {
		return errors.New("Socket is already open!")
	}

	socket, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	i.Socket = socket
	return nil
}

func HandleSignal(sig os.Signal, handler func(*Inspeqtor)) {
	SignalHandlers[sig] = handler
}

func handleSignals(i *Inspeqtor) {
	signals := make(chan os.Signal)
	for k, _ := range SignalHandlers {
		signal.Notify(signals, k)
	}

	for {
		sig := <-signals
		util.Debug("Received signal %d", sig)
		funk := SignalHandlers[sig]
		funk(i)
	}
}

func exit(i *Inspeqtor) {
	util.Info(Name + " exiting")
	if i.Socket != nil {
		err := i.Socket.Close()
		if err != nil {
			util.Warn(err.Error())
		}
	}
	os.Exit(0)
}

// this method never returns.
//
// since we can't test this method in an automated fashion, it should
// contain as little logic as possible.
func (i *Inspeqtor) runLoop() {
	util.DebugDebug("Resolving services")
	for _, svc := range i.Services {
		svc.Resolve(i.ServiceManagers)
	}

	i.scanSystem()

	for {
		select {
		case <-time.After(time.Duration(i.GlobalConfig.Top.CycleTime) * time.Second):
			i.scanSystem()
		}
	}
}

func (i *Inspeqtor) silenced() bool {
	return time.Now().Before(i.SilenceUntil)
}

func (i *Inspeqtor) scanSystem() {
	// "Trust, but verify"
	// https://en.wikipedia.org/wiki/Trust%2C_but_verify
	i.trust()
	i.verify()
}

func (i *Inspeqtor) trust() {
	start := time.Now()
	var barrier sync.WaitGroup
	barrier.Add(1)
	barrier.Add(len(i.Services))

	go i.Host.Collect(func(_ Checkable) {
		barrier.Done()
	})
	for _, svc := range i.Services {
		go svc.Collect(func(_ Checkable) {
			barrier.Done()
		})
	}
	barrier.Wait()
	util.Debug("Collection complete in " + time.Now().Sub(start).String())
}

func (i *Inspeqtor) verify() {
	if i.silenced() {
		// We are silenced until some point in the future.
		// We don't want to check rules (as a deploy might use
		// enough resources to trip a threshold) or trigger
		// any actions
		for _, rule := range i.Host.Rules() {
			rule.Reset()
		}
		for _, svc := range i.Services {
			for _, rule := range svc.Rules() {
				rule.Reset()
			}
		}
	} else {
		i.Host.Verify()
		for _, svc := range i.Services {
			svc.Verify()
		}
	}
}

/*
func (i *Inspeqtor) handleProcessEvent(etype EventType, svc Checkable) {
	if i.silenced() {
		util.Debug("SILENCED %s %s", etype, svc.Name())
		return
	}

	util.Warn("%s %s", etype, svc.Name())

	evt := Event{etype, svc, nil}
	err := svc.Trigger(&evt)
	if err != nil {
		util.Warn("%s", err)
	}
}

func (i *Inspeqtor) handleRuleEvent(etype EventType, check Checkable, rule *Rule) {
	if i.silenced() {
		util.Debug("SILENCED %s %s", etype, check.Name())
		return
	}

	util.Warn("%s %s", etype, check.Name())

	evt := Event{etype, check, rule}
	for _, action := range rule.Actions {
		err := action.Trigger(&evt)
		if err != nil {
			util.Warn("%s", err)
		}
	}
}
*/
func (i *Inspeqtor) TestNotifications() {
	for _, route := range i.GlobalConfig.AlertRoutes {
		nm := route.Name
		if nm == "" {
			nm = "default"
		}
		util.Info("Creating notification for %s/%s", route.Channel, nm)
		notifier, err := Actions["alert"](i.Host, route)
		if err != nil {
			util.Warn("Error creating %s/%s route: %s", route.Channel, nm, err.Error())
			continue
		}
		util.Info("Triggering notification for %s/%s", route.Channel, nm)
		err = notifier.Trigger(&Event{RuleFailed, i.Host, i.Host.Rules()[0]})
		if err != nil {
			util.Warn("Error firing %s/%s route: %s", route.Channel, nm, err.Error())
		}
	}
}
