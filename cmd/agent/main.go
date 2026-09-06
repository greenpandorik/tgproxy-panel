package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"tgwebproxy/internal/agent"
	"tgwebproxy/internal/logging"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Println(agent.Version)
			return
		case "init-node":
			fs := flag.NewFlagSet("init-node", flag.ExitOnError)
			engine := fs.String("engine", agent.EngineTProxy, "node engine: tproxy or telemt")
			cfgPath := fs.String("config", "/etc/tproxy-server/config.json", "relay config path")
			envPath := fs.String("mtproxy-env", "/etc/mtproxy/mtproxy.env", "mtproxy env path")
			dropin := fs.String("dropin", "/etc/systemd/system/mtproxy.service.d/secrets.conf", "systemd drop-in path")
			maxProfiles := fs.Int("max-profiles", 128, "relay max_profiles")

			hostname := fs.String("hostname", "", "telemt: public hostname of the node")
			publicIP := fs.String("public-ip", "", "telemt: public IP used for the WEB vhost")
			tlsDomain := fs.String("tls-domain", "", "telemt: Fake-TLS SNI domain (default: hostname)")
			classicPort := fs.Int("classic-port", 8443, "telemt: Fake-TLS listener port")
			webUser := fs.String("web-user", "", "telemt: initial user name")
			webSecret := fs.String("web-secret", "", "telemt: initial user secret (32 hex)")
			siteSrc := fs.String("site-dir", "", "telemt: source directory copied into the decoy site")
			noSynlimit := fs.Bool("no-synlimit", false, "telemt: omit the Fake-TLS listener's nftables synlimit (test benches without CAP_NET_ADMIN only)")
			noTLSEmulation := fs.Bool("no-tls-emulation", false, "telemt: set censorship.tls_emulation = false (test benches whose tls-domain does not resolve)")
			telemtConfig := fs.String("telemt-config", agent.DefaultTelemtConfigPath, "telemt: config path")
			telemtToken := fs.String("telemt-token-file", agent.DefaultTelemtTokenPath, "telemt: API token path")
			telemtUnit := fs.String("telemt-unit", agent.DefaultTelemtUnitPath, "telemt: systemd unit path")
			telemtSite := fs.String("telemt-site-dir", agent.DefaultTelemtSiteDir, "telemt: decoy site directory")
			telemtDataDir := fs.String("telemt-data-dir", agent.DefaultTelemtDataDir, "telemt: state directory")
			telemtBin := fs.String("telemt-bin", agent.DefaultTelemtBin, "telemt: binary path")
			stateDir := fs.String("state-dir", agent.DefaultStateDir, "agent state directory")
			_ = fs.Parse(os.Args[2:])

			if *engine == agent.EngineTelemt {
				tokenPath, err := agent.InitTelemtNode(context.Background(), agent.OSExec{}, agent.TelemtInitParams{
					Hostname: *hostname, PublicIP: *publicIP, TLSDomain: *tlsDomain, ClassicPort: *classicPort,
					WebUser: *webUser, WebSecret: *webSecret, SiteSrc: *siteSrc, NoSynlimit: *noSynlimit, NoTLSEmulation: *noTLSEmulation,
					ConfigPath: *telemtConfig, TokenPath: *telemtToken, UnitPath: *telemtUnit,
					SiteDir: *telemtSite, DataDir: *telemtDataDir, Binary: *telemtBin, StateDir: *stateDir,
				})
				if err != nil {
					fmt.Fprintln(os.Stderr, "init-node:", err)
					os.Exit(1)
				}
				// The token itself is never printed: the installer reads the file.
				fmt.Println("telemt node initialised; api token file:", tokenPath)
				return
			}
			if *engine != agent.EngineTProxy {
				fmt.Fprintf(os.Stderr, "init-node: unknown engine %q\n", *engine)
				os.Exit(2)
			}
			if err := agent.InitNode(*cfgPath, *envPath, *dropin, *maxProfiles); err != nil {
				fmt.Fprintln(os.Stderr, "init-node:", err)
				os.Exit(1)
			}
			fmt.Println("node initialised")
			return
		}
	}
	cfg, err := agent.LoadConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(2)
	}
	log := logging.New(os.Getenv("LOG_LEVEL"))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := agent.Run(ctx, cfg, agent.NewHandler(cfg, agent.OSExec{}, log), log); err != nil {
		log.Error("agent stopped", "err", err)
		os.Exit(1)
	}
}
