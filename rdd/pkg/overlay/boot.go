// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package overlay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// grubConfig is the boot menu on the image's root filesystem. The EFI system
// partition holds only a stub that reads this file, so the kernel command line
// the guest boots with is here.
const grubConfig = "/boot/grub2/grub.cfg"

// BootConfig is a distro whose kernel command line can be changed. The ext4
// image implements it; the WSL tarball boots no kernel of its own.
type BootConfig interface {
	AppendKernelParams(params string, mtime time.Time) error
}

// AppendKernelParams adds params to the kernel command line of every boot
// entry, after the one kiwi's kernelcmdline attribute put in the image.
func AppendKernelParams(d Distro, params string, mtime time.Time) error {
	if strings.TrimSpace(params) == "" {
		return errors.New("no kernel parameters to append")
	}
	if strings.ContainsAny(params, "\r\n") {
		return errors.New("kernel parameters cannot span lines")
	}
	b, ok := d.(BootConfig)
	if !ok {
		return errors.New("this distro format has no bootloader config")
	}
	return b.AppendKernelParams(params, mtime)
}

// AppendKernelParams rewrites the GRUB config, appending params to every entry.
// It keeps the file's owner and mode and stamps the overlay's mtime, so two
// builds of one commit still produce the same image.
func (i *imageDistro) AppendKernelParams(params string, mtime time.Time) error {
	r := rel(grubConfig)
	info, st, err := i.stat(r)
	if err != nil {
		return fmt.Errorf("%s: %w", grubConfig, err)
	}
	data, err := i.readFile(r)
	if err != nil {
		return fmt.Errorf("%s: %w", grubConfig, err)
	}
	patched, count := appendToLinuxLines(data, params)
	if count == 0 {
		return fmt.Errorf("%s has no linux line, so this is not the boot menu the tool knows", grubConfig)
	}
	return i.WriteFile(grubConfig, bytes.NewReader(patched), int(st.UID), int(st.GID), info.Mode(), mtime)
}

func (i *imageDistro) readFile(r string) ([]byte, error) {
	f, err := i.fs.OpenFile(r, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

// appendToLinuxLines appends params to every `linux` line and reports how many
// it changed. grub2-mkconfig writes one per menu entry, holding the kernel path
// and the whole command line.
func appendToLinuxLines(data []byte, params string) (patched []byte, count int) {
	lines := strings.Split(string(data), "\n")
	for n, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "linux" {
			continue
		}
		lines[n] = strings.TrimRight(line, " \t") + " " + params
		count++
	}
	return []byte(strings.Join(lines, "\n")), count
}
