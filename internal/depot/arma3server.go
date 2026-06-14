package depot

import (
	"arma3modmanager/internal/utils"
)

// dedicatedServerInstalled delegates to utils which understands steamapps/common layout.
func dedicatedServerInstalled(dir string) bool {
	return utils.DedicatedServerInstalled(dir)
}
