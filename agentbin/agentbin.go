// Package agentbin embeds the Linux agent binaries built by build.ps1,
// so the panel can install the agent on a server with one click.
package agentbin

import (
	"embed"
	"fmt"
)

//go:embed dist
var dist embed.FS

func Binary(arch string) ([]byte, error) {
	b, err := dist.ReadFile("dist/backupper-agent-linux-" + arch)
	if err != nil {
		return nil, fmt.Errorf("в сборку панели не вложен агент для linux/%s — соберите через build.ps1", arch)
	}
	return b, nil
}
