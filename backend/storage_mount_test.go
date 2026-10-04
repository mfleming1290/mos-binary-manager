package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// MOS bind-mounts persistent service data into system paths. A sibling on the
// same pool is still persistent storage, not a system-filesystem alias.
func mosServiceMounts(t *testing.T, poolRoot string) []mountRecord {
	t.Helper()
	mounts, err := parseMountInfo([]byte("1 0 0:1 / / rw - tmpfs root rw\n" +
		"2 1 8:1 / /boot rw - vfat /dev/sda rw\n" +
		"3 1 0:35 " + poolRoot + " /mnt/user rw - btrfs /dev/nvme0n1p2 rw\n" +
		"4 1 0:35 " + filepath.Join(poolRoot, "system/docker") + " /var/lib/docker rw - btrfs /dev/nvme0n1p2 rw\n" +
		"5 1 0:35 " + filepath.Join(poolRoot, "system/lxc") + " /var/lib/lxc rw - btrfs /dev/nvme0n1p2 rw\n" +
		"6 1 0:35 " + filepath.Join(poolRoot, "system/libvirt") + " /etc/libvirt rw - btrfs /dev/nvme0n1p2 rw\n"))
	if err != nil {
		t.Fatal(err)
	}
	return mounts
}

func TestPersistentPoolAllowsSiblingsOfSystemDataBinds(t *testing.T) {
	for _, root := range []string{"/", "/@pool"} {
		t.Run(root, func(t *testing.T) {
			mounts := mosServiceMounts(t, root)
			for _, path := range []string{"/mnt/user/binary-manager", "/mnt/user/binary-manager/one/home", "/mnt/user/appdata/binary-manager", "/mnt/user/system/docker-other/home"} {
				if m, err := persistentMount(path, mounts); err != nil || m.ID != "3" {
					t.Errorf("native pool sibling %s rejected: %v", path, err)
				}
			}
		})
	}
}

func TestPersistentPoolStillRejectsSystemDataAndAliases(t *testing.T) {
	for _, root := range []string{"/", "/@pool"} {
		t.Run(root, func(t *testing.T) {
			mounts := mosServiceMounts(t, root)
			mounts = append(mounts, mountRecord{ID: "7", Device: "0:35", Root: filepath.Join(root, "system/docker"), Point: "/mnt/docker-alias", FSType: "btrfs"})
			for _, path := range []string{"/mnt/user", "/mnt/user/system", "/mnt/user/system/docker", "/mnt/user/system/docker/child", "/mnt/user/system/lxc/child", "/mnt/user/system/libvirt/child", "/mnt/docker-alias/home", "/var/lib/docker/home", "/etc/libvirt/home"} {
				if _, err := persistentMount(path, mounts); err == nil {
					t.Errorf("accepted system-data overlap or alias %s", path)
				}
			}
		})
	}
}

func TestPersistentPoolBindUsesFilesystemRoot(t *testing.T) {
	mounts := mosServiceMounts(t, "/@pool")
	mounts = append(mounts, mountRecord{ID: "7", Device: "0:35", Root: "/@pool/private", Point: "/mnt/private", FSType: "btrfs"})
	if m, err := persistentMount("/mnt/private/homes/one/home", mounts); err != nil || m.ID != "7" {
		t.Fatalf("unrelated bind failed: %v", err)
	}
	// A different Btrfs subvolume on the same filesystem is not the service root.
	mounts = append(mounts, mountRecord{ID: "8", Device: "0:35", Root: "/@homes", Point: "/mnt/homes", FSType: "btrfs"})
	if _, err := persistentMount("/mnt/homes/one/home", mounts); err != nil {
		t.Fatalf("unrelated subvolume failed: %v", err)
	}
}

func TestPersistentPoolBootAndRootDevicesStayBlocked(t *testing.T) {
	mounts := mosServiceMounts(t, "/")
	mounts = append(mounts,
		mountRecord{ID: "7", Device: "8:1", Root: "/elsewhere", Point: "/mnt/boot-alias", FSType: "ext4"},
		mountRecord{ID: "8", Device: "0:1", Root: "/elsewhere", Point: "/mnt/root-alias", FSType: "ext4"})
	for _, path := range []string{"/mnt/boot-alias/home", "/mnt/root-alias/home", "/mnt/missing/home"} {
		if _, err := persistentMount(path, mounts); err == nil {
			t.Fatalf("accepted boot/root/fallback: %s", path)
		}
	}
}

func TestPersistentPoolNonBtrfsSystemDevicesStayBlocked(t *testing.T) {
	for _, fs := range []string{"ext4", "xfs", "zfs", "bcachefs", "nfs", "cifs"} {
		t.Run(fs, func(t *testing.T) {
			mounts := mosServiceMounts(t, "/")
			for i := range mounts {
				if mounts[i].Device == "0:35" {
					mounts[i].FSType = fs
				}
			}
			for _, path := range []string{"/mnt/user/binary-manager", "/mnt/user/SYSTEM/DOCKER/home"} {
				if _, err := persistentMount(path, mounts); err == nil {
					t.Fatalf("relaxed unverified filesystem naming rules for %s", path)
				}
			}
		})
	}
}

func TestPersistentPoolSystemRootsMustBeKnown(t *testing.T) {
	for _, index := range []int{2, 3} {
		for _, root := range []string{"", "relative", "/system/./docker", "/system/docker/", "/system/lxc/../docker", "//system/docker"} {
			mounts := mosServiceMounts(t, "/")
			mounts[index].Root = root
			if _, err := persistentMount("/mnt/user/binary-manager", mounts); err == nil {
				t.Fatalf("accepted unverified filesystem root %q for mount %d", root, index)
			}
		}
	}
}

func TestPersistentPoolSystemRootOnOtherDeviceDoesNotBlock(t *testing.T) {
	mounts := mosServiceMounts(t, "/")
	for i := 3; i < len(mounts); i++ {
		mounts[i].Device = "different-pool"
	}
	if _, err := persistentMount("/mnt/user/system/docker/home", mounts); err != nil {
		t.Fatalf("unrelated filesystem names collided: %v", err)
	}
}

func TestManagedHomePoolWithServiceBindNoFallback(t *testing.T) {
	pool, mounts := simulatedPool(t)
	mounts[1].Root = "/"
	mounts[1].FSType = "btrfs"
	mounts = append(mounts, mountRecord{ID: "service", Device: "pool", Root: "/system/docker", Point: "/var/lib/docker", FSType: "btrfs"})
	storage := filepath.Join(pool, "binary-manager")
	home := filepath.Join(storage, "one", "home")
	if err := persistentHome(home, storage, true, mounts); err != nil {
		t.Fatalf("private sibling creation failed: %v", err)
	}
	// Keeping the system bind alone must never make an unmounted pool writable.
	if err := persistentHome(home, storage, true, []mountRecord{mounts[0], mounts[2]}); err == nil || !strings.Contains(err.Error(), "refusing root-filesystem fallback") {
		t.Fatalf("missing pool was not safely rejected: %v", err)
	}
}
