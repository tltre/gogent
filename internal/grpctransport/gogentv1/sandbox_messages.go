package gogentv1

type ListSandboxesRequest struct{}

type SandboxInfoList struct {
	Items []*SandboxInfo
}

type CreateSandboxRequest struct {
	Name      string
	Profile   string
	Lifecycle string
	TimeoutMs int64
}

type SandboxInfo struct {
	Name    string
	Profile string
	Status  string
}

type GetSandboxRequest struct {
	SandboxName string
}

type DestroySandboxRequest struct {
	SandboxName string
}

type DestroySandboxResponse struct {
	Success bool
}

type ExecInSandboxRequest struct {
	SandboxName string
	Code        string
	Language    string
	TimeoutMs   int64
	Env         map[string]string
}

type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int32
	Error    string
}
