// Copyright (C) 2026, Zoo Labs Foundation. All rights reserved.
// See the file LICENSE for licensing terms.

// zoo-evm is the Zoo Network node -- a sovereign L1/L2 on the Lux Network
// running the standard Lux EVM (no custom precompiles).
//
// Usage:
//
//	zoo-evm                Run the node (default)
//	zoo-evm version        Print version info
//
// When invoked as a VM subprocess (LUX_VM_TRANSPORT set), it enters EVM
// plugin mode. This lets a single binary serve as both the node process
// and the EVM plugin.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/luxfi/evm/plugin/evm"
	"github.com/luxfi/evm/plugin/runner"
	"github.com/luxfi/node/app"
	"github.com/luxfi/node/config"
	nodeversion "github.com/luxfi/node/version"
	luxversion "github.com/luxfi/version"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

// version is the zoo-evm release version.
const version = "0.1.0"

const header = `
 ______  ___   ___
|___  / / _ \ / _ \
   / / | | | | | | |
  / /  | |_| | |_| |
 /___| \___/ \___/
`

// Treasury address placeholder. Deploy from m/44'/60'/0'/0/0 of ZOO_MNEMONIC.
// Genesis alloc uses 0x0000000000000000000000000000000000000000 until real
// treasury address is derived.

func main() {
	// VM subprocess mode -- the node launched us as the EVM plugin.
	if os.Getenv("LUX_VM_TRANSPORT") != "" {
		versionStr := fmt.Sprintf("Zoo-EVM/%s [node=%s, rpcchainvm=%d]",
			evm.Version, luxversion.Current, luxversion.RPCChainVMProtocol)
		if len(os.Args) > 1 && os.Args[1] == "version" {
			fmt.Println(versionStr)
			os.Exit(0)
		}
		runner.Run(versionStr)
		return
	}

	// Subcommand dispatch.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			printVersion()
			return
		}
	}

	runNode()
}

func printVersion() {
	versions := nodeversion.GetVersions()
	fmt.Printf("zoo-evm %s (luxd %s)\n", version, versions.String())
}

func runNode() {
	fs := config.BuildFlagSet()
	v, err := config.BuildViper(fs, os.Args[1:])
	if errors.Is(err, pflag.ErrHelp) {
		os.Exit(0)
	}
	if err != nil {
		fmt.Printf("couldn't configure flags: %s\n", err)
		os.Exit(1)
	}

	if v.GetBool(config.VersionKey) {
		fmt.Println(nodeversion.GetVersions().String())
		os.Exit(0)
	}

	nodeConfig, err := config.GetNodeConfig(v)
	if err != nil {
		fmt.Printf("couldn't load node config: %s\n", err)
		os.Exit(1)
	}

	// Install self as the standard EVM plugin so the node discovers us.
	if err := installPlugin(nodeConfig.PluginDir, evm.ID.String()); err != nil {
		fmt.Printf("couldn't install EVM plugin: %s\n", err)
		os.Exit(1)
	}

	if term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Print(header)
	}

	nodeApp, err := app.New(nodeConfig)
	if err != nil {
		fmt.Printf("couldn't start node: %s\n", err)
		os.Exit(1)
	}

	os.Exit(app.Run(nodeApp))
}

// installPlugin creates a symlink of this binary in the plugin directory
// under the given VM ID name, so the node's VMRegistry discovers it.
func installPlugin(pluginDir string, vmIDStr string) error {
	if pluginDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("couldn't get home dir: %w", err)
		}
		pluginDir = filepath.Join(home, ".lux", "plugins")
	}
	if err := os.MkdirAll(pluginDir, 0700); err != nil {
		return fmt.Errorf("couldn't create plugin dir: %w", err)
	}

	pluginPath := filepath.Join(pluginDir, vmIDStr)
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("couldn't get executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("couldn't resolve executable path: %w", err)
	}

	// Atomic install: create temp symlink, then rename (atomic on POSIX).
	tmpPath := pluginPath + ".tmp." + strconv.Itoa(os.Getpid())
	_ = os.Remove(tmpPath)
	if err := os.Symlink(exe, tmpPath); err != nil {
		// Fallback to hard link if symlink fails (e.g., cross-device).
		if err := os.Link(exe, tmpPath); err != nil {
			return fmt.Errorf("couldn't link plugin binary: %w", err)
		}
	}
	if err := os.Rename(tmpPath, pluginPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("couldn't install plugin atomically: %w", err)
	}
	return nil
}
