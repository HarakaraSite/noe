//go:build windows

package processscope

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsScope struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	job      windows.Handle
	assigned bool
}

func newScope(cmd *exec.Cmd) (*windowsScope, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	attr := cmd.SysProcAttr
	if attr == nil {
		attr = &syscall.SysProcAttr{}
	} else {
		copy := *attr
		attr = &copy
	}
	attr.CreationFlags |= windows.CREATE_SUSPENDED
	cmd.SysProcAttr = attr
	return &windowsScope{cmd: cmd, job: job}, nil
}

func (s *windowsScope) Start() error {
	if err := s.cmd.Start(); err != nil {
		return err
	}
	var assignErr error
	err := s.cmd.Process.WithHandle(func(handle uintptr) {
		assignErr = windows.AssignProcessToJobObject(s.job, windows.Handle(handle))
	})
	if err != nil {
		s.abortStart()
		return err
	}
	if assignErr != nil {
		s.abortStart()
		return assignErr
	}
	s.assigned = true
	if err := resumeSuspendedProcess(uint32(s.cmd.Process.Pid)); err != nil {
		s.abortStart()
		return err
	}
	return nil
}

func (s *windowsScope) abortStart() {
	if s.job != 0 {
		_ = windows.TerminateJobObject(s.job, 1)
	}
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

func (s *windowsScope) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

func (s *windowsScope) Signal(name string) error {
	if err := validateSignal(name); err != nil {
		return err
	}
	return s.Kill()
}

func validateSignal(name string) error {
	name = strings.ToUpper(name)
	if name != "SIGTERM" && name != "SIGKILL" {
		return errors.New("unsupported Windows process signal: " + name)
	}
	return nil
}

func (s *windowsScope) Kill() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job == 0 || !s.assigned {
		return nil
	}
	err := windows.TerminateJobObject(s.job, 1)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return nil
	}
	return err
}

func (s *windowsScope) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job == 0 {
		return nil
	}
	job := s.job
	s.job = 0
	s.assigned = false
	return windows.CloseHandle(job)
}

func killGroup(pid int, signal string) error {
	return errors.New("process groups are unavailable on Windows")
}

func waitExit(process *os.Process) error {
	return errors.New("unreaped process waiting is unavailable on Windows")
}

func resumeSuspendedProcess(processID uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	err = windows.Thread32First(snapshot, &entry)
	for err == nil {
		if entry.OwnerProcessID == processID {
			thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if err != nil {
				return err
			}
			_, resumeErr := windows.ResumeThread(thread)
			closeErr := windows.CloseHandle(thread)
			return errors.Join(resumeErr, closeErr)
		}
		entry.Size = uint32(unsafe.Sizeof(windows.ThreadEntry32{}))
		err = windows.Thread32Next(snapshot, &entry)
	}
	if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return errors.New("suspended process thread was not found")
	}
	return err
}
