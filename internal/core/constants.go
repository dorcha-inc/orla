package core

import "fmt"

const (
	MaintainerLink    = "https://github.com/dorcha-inc/orla/blob/main/MAINTAINERS.md"
	BugReportTemplate = "\n\n[NOTE]This is most likely a bug in orla, please reach out to the maintainers at %s"
)

func BugReportMessage() string {
	return fmt.Sprintf(BugReportTemplate, MaintainerLink)
}

const (
	GOOSDarwin  = "darwin"
	GOOSLinux   = "linux"
	GOOSWindows = "windows"
)
