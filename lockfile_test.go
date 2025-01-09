package lockfile

import (
	"fmt"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func ExampleLockfile() {
	lock, err := New(filepath.Join(os.TempDir(), "lock.me.now.lck"))
	if err != nil {
		fmt.Printf("Cannot init lock. reason: %v", err)
		panic(err) // handle properly please!
	}

	// Error handling is essential, as we only try to get the lock.
	if err = lock.TryLock("main"); err != nil {
		fmt.Printf("Cannot lock %q, reason: %v", lock, err)
		panic(err) // handle properly please!
	}

	defer func() {
		if err := lock.Unlock(); err != nil {
			fmt.Printf("Cannot unlock %q, reason: %v", lock, err)
			panic(err) // handle properly please!
		}
	}()

	fmt.Println("Do stuff under lock")
	// Output: Do stuff under lock
}

func TestBasicLockUnlock(t *testing.T) {
	path, err := filepath.Abs("test_lockfile.pid")
	if err != nil {
		panic(err)
	}

	lf, err := New(path)
	if err != nil {
		t.Fail()
		fmt.Println("Error making lockfile: ", err)
		return
	}

	err = lf.TryLock("main")
	if err != nil {
		t.Fail()
		fmt.Println("Error locking lockfile: ", err)
		return
	}

	err = lf.Unlock()
	if err != nil {
		t.Fail()
		fmt.Println("Error unlocking lockfile: ", err)
		return
	}
}

func GetDeadPID() int {
	// I have no idea how windows handles large PIDs, or if they even exist.
	// So limit it to be less or equal to 4096 to be safe.
	const maxPid = 4095

	// limited iteration, so we finish one day
	seen := map[int]bool{}
	for len(seen) < maxPid {
		pid := rand.Intn(maxPid + 1) // see https://godoc.org/math/rand#Intn why
		if seen[pid] {
			continue
		}

		seen[pid] = true

		running, err := isRunning(pid)
		if err != nil {
			fmt.Println("Error checking PID: ", err)
			continue
		}

		if !running {
			return pid
		}
	}
	panic(fmt.Sprintf("all pids lower %d are used, cannot test this", maxPid))
}

func TestBusy(t *testing.T) {
	path, err := filepath.Abs("test_lockfile.pid")
	if err != nil {
		t.Fatal(err)
		return
	}

	pid := os.Getppid()

	if err := ioutil.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0666); err != nil {
		t.Fatal(err)
		return
	}
	defer os.Remove(path)

	lf, err := New(path)
	if err != nil {
		t.Fatal(err)
		return
	}

	got := lf.TryLock("main")
	if got != ErrBusy {
		t.Fatalf("expected error %q, got %v", ErrBusy, got)
		return
	}
}

func TestRogueDeletion(t *testing.T) {
	path, err := filepath.Abs("test_lockfile.pid")
	if err != nil {
		t.Fatal(err)
		return
	}
	lf, err := New(path)
	if err != nil {
		t.Fatal(err)
		return
	}
	err = lf.TryLock("main")
	if err != nil {
		t.Fatal(err)
		return
	}
	err = os.Remove(path)
	if err != nil {
		t.Fatal(err)
		return
	}

	got := lf.Unlock()
	if got != ErrRogueDeletion {
		t.Fatalf("unexpected error: %v", got)
		return
	}
}

func TestRogueDeletionDeadPid(t *testing.T) {
	path, err := filepath.Abs("test_lockfile.pid")
	if err != nil {
		t.Fatal(err)
		return
	}
	lf, err := New(path)
	if err != nil {
		t.Fatal(err)
		return
	}
	err = lf.TryLock("main")
	if err != nil {
		t.Fatal(err)
		return
	}

	pid := GetDeadPID()
	if err := ioutil.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0666); err != nil {
		t.Fatal(err)
		return
	}

	defer os.Remove(path)

	err = lf.Unlock()
	if err != ErrRogueDeletion {
		t.Fatalf("unexpected error: %v", err)
		return
	}

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			content, _ := ioutil.ReadFile(path)
			t.Fatalf("lockfile %q (%q) should not be deleted by us, if we didn't create it", path, content)
		}
		t.Fatalf("unexpected error %v", err)
	}
}

func TestRemovesStaleLockOnDeadOwner(t *testing.T) {
	path, err := filepath.Abs("test_lockfile.pid")
	if err != nil {
		t.Fatal(err)
		return
	}
	lf, err := New(path)
	if err != nil {
		t.Fatal(err)
		return
	}
	pid := GetDeadPID()
	if err := ioutil.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0666); err != nil {
		t.Fatal(err)
		return
	}
	err = lf.TryLock("main")
	if err != nil {
		t.Fatal(err)
		return
	}

	if err := lf.Unlock(); err != nil {
		t.Fatal(err)
		return
	}
}

func TestInvalidPidLeadToReplacedLockfileAndSuccess(t *testing.T) {
	path, err := filepath.Abs("test_lockfile.pid")
	if err != nil {
		t.Fatal(err)
		return
	}

	if err := ioutil.WriteFile(path, []byte("\n"), 0666); err != nil {
		t.Fatal(err)
		return
	}

	defer os.Remove(path)

	lf, err := New(path)
	if err != nil {
		t.Fatal(err)
		return
	}

	if err := lf.TryLock("main"); err != nil {
		t.Fatalf("unexpected error: %v", err)
		return
	}

	// now check if file exists and contains the correct content
	got, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
		return
	}
	want := fmt.Sprintf("%d\n", os.Getpid())
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestScanPidLine(t *testing.T) {
	tests := [...]struct {
		input []byte
		pid   int
		xfail error
	}{
		{xfail: ErrInvalidPid},
		{input: []byte(""), xfail: ErrInvalidPid},
		{input: []byte("\n"), xfail: ErrInvalidPid},
		{input: []byte("-1\n"), xfail: ErrInvalidPid},
		{input: []byte("0\n"), xfail: ErrInvalidPid},
		{input: []byte("a\n"), xfail: ErrInvalidPid},
		{input: []byte("1\n"), pid: 1},
	}

	// test positive cases first
	for step, tc := range tests {
		if tc.xfail != nil {
			continue
		}

		got, err := scanPidLine(tc.input)
		if err != nil {
			t.Fatalf("%d: unexpected error %v", step, err)
		}

		if want := tc.pid; got != want {
			t.Errorf("%d: expected pid %d, got %d", step, want, got)
		}
	}

	// test negative cases now
	for step, tc := range tests {
		if tc.xfail == nil {
			continue
		}

		_, got := scanPidLine(tc.input)
		if want := tc.xfail; got != want {
			t.Errorf("%d: expected error %v, got %v", step, want, got)
		}
	}
}

// custom

func TestTryLock_Success(t *testing.T) {
	lock, err := New("/tmp/test.lock")
	assert.NoError(t, err)

	err = lock.TryLock("testprocess")
	assert.NoError(t, err)
}

func TestTryLock_LockedByOtherProcess(t *testing.T) {
	// Simulate an external process holding the lock
	lock, err := New("/tmp/test.lock")
	assert.NoError(t, err)

	// Simulate a process holding the lock
	existingProc := &process.Process{
		Pid: 1234, // Example PID of an external process
	}
	// Set up the lock file to mimic another process holding the lock
	lockContent := fmt.Sprintf("%d\n", existingProc.Pid)
	err = os.WriteFile("/tmp/test.lock", []byte(lockContent), 0644)
	assert.NoError(t, err)

	err = lock.TryLock("testprocess")
	assert.EqualError(t, err, ErrBusy.Error())
}

func TestTryLock_DeadProcess(t *testing.T) {
	// Simulate a dead process
	lock, err := New("/tmp/test.lock")
	assert.NoError(t, err)

	// Write a dead process' PID to the lock file
	deadProcPid := 9999
	err = os.WriteFile("/tmp/test.lock", []byte(fmt.Sprintf("%d\n", deadProcPid)), 0644)
	assert.NoError(t, err)

	// Simulate the process being dead
	err = lock.TryLock("testprocess")
	assert.NoError(t, err)
}

func TestUnlock_Success(t *testing.T) {
	lock, err := New("/tmp/test.lock")
	assert.NoError(t, err)

	err = lock.TryLock("testprocess")
	assert.NoError(t, err)

	err = lock.Unlock()
	assert.NoError(t, err)

	// Verify that the lock file was deleted
	_, err = os.Stat("/tmp/test.lock")
	assert.True(t, os.IsNotExist(err))
}

func TestUnlock_RogueDeletion(t *testing.T) {
	lock, err := New("/tmp/test.lock")
	assert.NoError(t, err)

	// Try unlocking without having the lock (simulate rogue deletion)
	err = lock.Unlock()
	assert.EqualError(t, err, ErrRogueDeletion.Error())
}

func TestTryLock_DifferentProcessName(t *testing.T) {
	// Simulate an attempt to lock with a different process name
	lock, err := New("/tmp/test.lock")
	assert.NoError(t, err)

	// Simulate an existing process holding the lock with a different process name
	existingProc := &process.Process{
		Pid: 1234,
	}
	lockContent := fmt.Sprintf("%d\n", existingProc.Pid)
	err = os.WriteFile("/tmp/test.lock", []byte(lockContent), 0644)
	assert.NoError(t, err)

	err = lock.TryLock("anotherprocess")
	assert.NoError(t, err) // Since the process name doesn't match, we should be able to acquire the lock.
}
