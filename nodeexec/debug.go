package nodeexec

import (
	"fmt"
	"os/exec"
)

// DefaultDebugImage is the container image used for node debug sessions.
const DefaultDebugImage = "nicolaka/netshoot"

// NodeDebugCmd returns an exec.Cmd that opens an interactive shell on the
// given node using `kubectl debug node/<name>`.
//
// There is no client-go/SDK equivalent for `kubectl debug node/` — it creates
// a privileged pod with host namespaces (PID/network/IPC) which requires the
// full kubectl-side logic to set up correctly. kubectl is therefore required
// for this operation.
//
// image defaults to DefaultDebugImage when empty.
// namespace is the namespace in which the debug pod is created; "" = server default.
func NodeDebugCmd(nodeName, image, namespace string) *exec.Cmd {
	if image == "" {
		image = DefaultDebugImage
	}
	args := []string{
		"debug",
		fmt.Sprintf("node/%s", nodeName),
		"--stdin",
		"--tty",
		"--image=" + image,
		"--profile=sysadmin",
	}
	if namespace != "" {
		args = append(args, "--namespace="+namespace)
	}
	args = append(args, "--", "bash")
	return exec.Command("kubectl", args...)
}

// KubectlAvailable returns true if kubectl is found in PATH.
func KubectlAvailable() bool {
	_, err := exec.LookPath("kubectl")
	return err == nil
}
