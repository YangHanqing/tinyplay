// Package sysvolume controls the OS output volume, not mpv's own gain.
//
// mpv's "volume" property is software gain layered on top of whatever the
// system output level already is; letting the phone's slider drive both
// would mean two independent volume controls fighting each other. Instead
// the slider drives this package directly, and mpv is pinned at unity gain
// (see player.go: --volume=100 / set_property volume 100).
package sysvolume

import "github.com/itchyny/volume-go"

func Get() (int, error) {
	return volume.GetVolume()
}

func Set(v int) (int, error) {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	if err := volume.SetVolume(v); err != nil {
		return 0, err
	}
	return v, nil
}

func GetMuted() (bool, error) {
	return volume.GetMuted()
}

func SetMuted(muted bool) (bool, error) {
	var err error
	if muted {
		err = volume.Mute()
	} else {
		err = volume.Unmute()
	}
	if err != nil {
		return false, err
	}
	return muted, nil
}
