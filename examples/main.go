package main

import (
	"fmt"
	"github.com/nightlyone/lockfile"
	"os"
	"path/filepath"
	"time"
)

func main() {
	expProcName := "main"
	lock, err := lockfile.New(filepath.Join(os.TempDir(), "lock.me.now.lck"))
	if err != nil {
		fmt.Printf("Cannot init lock. reason: %v", err)
		panic(err) // handle properly please!
	}

	// Error handling is essential, as we only try to get the lock.
	if err = lock.TryLock(expProcName); err != nil {
		fmt.Printf("Cannot lock %q, reason: %v", lock, err)
		panic(err) // handle properly please!
	}

	defer func() {
		if err := lock.Unlock(); err != nil {
			fmt.Printf("Cannot unlock %q, reason: %v", lock, err)
			panic(err) // handle properly please!
		}
	}()

	for {
		fmt.Printf("Locked %q\n", lock)
		time.Sleep(time.Second * 5)
	}

}
