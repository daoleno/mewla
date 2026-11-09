//go:build windows

package platform

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Job is a Windows Job Object that holds a process tree: every process the
// first one starts joins it, and Terminate or Close ends them all. It is the
// Windows counterpart of a Unix process group.
type Job struct {
	handle windows.Handle
}

// NewJob makes a job whose processes all end when the job's last handle
// closes (kill on close), and that lets its processes start children outside
// it only when they ask to break away.
func NewJob() (*Job, error) {
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK,
		},
	}
	if _, err := windows.SetInformationJobObject(handle, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("configure job object: %w", err)
	}
	return &Job{handle: handle}, nil
}

// Handle is the job's handle, for passing to a successor process.
func (j *Job) Handle() windows.Handle { return j.handle }

// AssignPID puts a running process (and the children it starts from now on)
// in the job.
func (j *Job) AssignPID(pid int) error {
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(process)
	return j.AssignHandle(process)
}

// AssignHandle puts the process behind handle in the job.
func (j *Job) AssignHandle(process windows.Handle) error {
	if err := windows.AssignProcessToJobObject(j.handle, process); err != nil {
		return fmt.Errorf("assign process to job: %w", err)
	}
	return nil
}

// AssignProcess puts p in the job.
func (j *Job) AssignProcess(p *os.Process) error { return j.AssignPID(p.Pid) }

// Terminate ends every process in the job with code.
func (j *Job) Terminate(code uint32) error {
	return windows.TerminateJobObject(j.handle, code)
}

// PIDs lists the processes in the job now.
func (j *Job) PIDs() ([]int, error) {
	// JOBOBJECT_BASIC_PROCESS_ID_LIST: two uint32 counts, then pids.
	const max = 512
	buf := make([]uintptr, 1+max)
	var returned uint32
	if err := windows.QueryInformationJobObject(j.handle, windows.JobObjectBasicProcessIdList,
		uintptr(unsafe.Pointer(&buf[0])), uint32(len(buf)*int(unsafe.Sizeof(buf[0]))), &returned); err != nil {
		return nil, fmt.Errorf("list job processes: %w", err)
	}
	counts := (*[2]uint32)(unsafe.Pointer(&buf[0]))
	n := int(counts[1])
	if n > max {
		n = max
	}
	pids := make([]int, 0, n)
	for i := 0; i < n; i++ {
		pids = append(pids, int(buf[1+i]))
	}
	return pids, nil
}

// Close releases this handle; with kill on close, the job's processes end
// when no handle remains.
func (j *Job) Close() error {
	if j == nil || j.handle == 0 {
		return nil
	}
	err := windows.CloseHandle(j.handle)
	j.handle = 0
	return err
}
