//go:build windows

package main

import (
	"strconv"

	"golang.org/x/sys/windows/registry"

	"github.com/nOmnomNOM3/A2-Client-Launcher/core"
)

// allods2.exe (32-bit, no manifest) keeps its options under
// HKLM\SOFTWARE\1C\Allods 2. Run without admin rights, Windows redirects
// its writes to the per-user VirtualStore copy and reads that copy first, so
// that is the copy we edit. The HKLM key is updated too when it is writable
// (launcher run as admin), for games that are started as admin.
const (
	regGame = `SOFTWARE\1C\Allods 2`
	regVS   = `Software\Classes\VirtualStore\MACHINE\SOFTWARE\WOW6432Node\1C\Allods 2`
)

type gameReg struct{}

func openRead() []registry.Key {
	var ks []registry.Key
	if k, err := registry.OpenKey(registry.CURRENT_USER, regVS, registry.QUERY_VALUE); err == nil {
		ks = append(ks, k)
	}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, regGame, registry.QUERY_VALUE|registry.WOW64_32KEY); err == nil {
		ks = append(ks, k)
	}
	return ks
}

// newGameReg returns nil when the game has never stored its options.
func newGameReg() core.KV {
	ks := openRead()
	for _, k := range ks {
		k.Close()
	}
	if len(ks) == 0 {
		return nil
	}
	return gameReg{}
}

func (gameReg) Get(name string) (string, bool) {
	for _, k := range openRead() {
		v, t, err := k.GetIntegerValue(name)
		k.Close()
		if err == nil && t == registry.DWORD {
			return strconv.FormatInt(int64(int32(v)), 10), true
		}
	}
	return "", false
}

func (gameReg) Set(name, val string) {
	n, err := strconv.ParseInt(val, 10, 32)
	if err != nil {
		return
	}
	if k, _, err := registry.CreateKey(registry.CURRENT_USER, regVS, registry.SET_VALUE); err == nil {
		k.SetDWordValue(name, uint32(n))
		k.Close()
	}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, regGame, registry.SET_VALUE|registry.WOW64_32KEY); err == nil {
		k.SetDWordValue(name, uint32(n))
		k.Close()
	}
}

// regBackup snapshots every value the settings screen can change.
func regBackup() map[string]uint32 {
	out := map[string]uint32{}
	for _, s := range core.NewSettings() {
		if s.Store != core.Reg {
			continue
		}
		for _, ch := range s.Choices {
			for k := range ch.Set {
				if v, ok := (gameReg{}).Get(k); ok {
					n, _ := strconv.ParseInt(v, 10, 32)
					out[k] = uint32(n)
				}
			}
		}
	}
	return out
}
