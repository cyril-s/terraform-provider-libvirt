package libvirt

import (
	"fmt"
	"golang.org/x/sys/unix"
	"io/ioutil"
	"os"
)

const (
	loopControlPath   = "/dev/loop-control"
	loopDevicePattern = "/dev/loop%d"
)

// LoopDevice represents loopback device on the filesystem with Device field
// storing path to loop device itself and BackingFile to the backing file
type LoopDevice struct {
	Device, BackingFile string
}

// NewLoopDevice creates sparse file of specified size and associates it with
// a free loop device. dir is path to directory where file will be created. Empty
// string for dir means 'use default value of ioutil.TempFile'. pattern is a base
// filename to which random string will be appended.
func NewLoopDevice(dir, pattern string, size int64) (*LoopDevice, error) {
	loopControl, err := os.Open(loopControlPath)
	if err != nil {
		return nil, err
	}
	defer loopControl.Close()

	freeLoopDevIndex, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		loopControl.Fd(),
		unix.LOOP_CTL_GET_FREE,
		0,
	)
	if errno != 0 {
		return nil, errno
	}
	loopDeviceName := fmt.Sprintf(loopDevicePattern, freeLoopDevIndex)

	// loop device must be opened with O_RDWR to be read-write after call to LOOP_SET_FD ioctl
	loopDevice, err := os.OpenFile(loopDeviceName, os.O_RDWR, 0660)
	if err != nil {
		return nil, err
	}
	defer loopDevice.Close()

	tmpfile, err := ioutil.TempFile(dir, pattern)
	if err != nil {
		return nil, err
	}
	defer tmpfile.Close()

	if err := tmpfile.Truncate(size); err != nil {
		os.Remove(tmpfile.Name())
		return nil, err
	}

	err = unix.IoctlSetInt(
		int(loopDevice.Fd()),
		unix.LOOP_SET_FD,
		int(tmpfile.Fd()),
	)
	if err != nil {
		os.Remove(tmpfile.Name())
		return nil, err
	}

	return &LoopDevice{loopDevice.Name(), tmpfile.Name()}, nil
}

// Destroy dissociates backing file from loopback device and removes
// it from the filesystem
func (device *LoopDevice) Destroy() error {
	loopDevice, err := os.Open(device.Device)
	if err != nil {
		return err
	}
	defer loopDevice.Close()

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, loopDevice.Fd(), unix.LOOP_CLR_FD, 0)
	if errno != 0 {
		return errno
	}

	if err := os.Remove(device.BackingFile); err != nil {
		return err
	}

	return nil
}
