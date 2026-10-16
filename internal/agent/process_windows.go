//go:build windows

package agent

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsProcessScope struct {
	cmd      *exec.Cmd
	job      windows.Handle
	assigned bool
}

func newProcessScope(cmd *exec.Cmd) (processScope, error) {
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
	return &windowsProcessScope{cmd: cmd, job: job}, nil
}

func (s *windowsProcessScope) start() error {
	if err := s.cmd.Start(); err != nil {
		return err
	}
	var assignErr error
	err := s.cmd.Process.WithHandle(func(handle uintptr) {
		assignErr = windows.AssignProcessToJobObject(s.job, windows.Handle(handle))
	})
	if err != nil {
		return err
	}
	if assignErr != nil {
		return assignErr
	}
	s.assigned = true
	return resumeSuspendedProcess(uint32(s.cmd.Process.Pid))
}

func (s *windowsProcessScope) terminate() error {
	if s.job == 0 || !s.assigned {
		return nil
	}
	return windows.TerminateJobObject(s.job, 1)
}

func (s *windowsProcessScope) close() error {
	if s.job == 0 {
		return nil
	}
	job := s.job
	s.job = 0
	return windows.CloseHandle(job)
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
		return errors.New("suspended child thread was not found")
	}
	return err
}

func sameEnvironmentKey(got, want string) bool { return strings.EqualFold(got, want) }
