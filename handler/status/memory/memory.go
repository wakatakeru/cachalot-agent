package memory

import (
	"fmt"
	"net/http"
	
	"github.com/shirou/gopsutil/mem"
)

func GetUsedPercent(w http.ResponseWriter, r *http.Request) {
	v, _ := mem.VirtualMemory()
	fmt.Fprintf(w, "%f\n", v.UsedPercent)
}
