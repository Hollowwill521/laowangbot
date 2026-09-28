package pluginapi

import "os"

func signals() []os.Signal { return []os.Signal{os.Interrupt} }
