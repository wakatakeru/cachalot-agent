package load

import (
	"fmt"
	"net/http"
	
	"github.com/shirou/gopsutil/load"
)

func GetLoad1(w http.ResponseWriter, r *http.Request) {
	loadAvg, _ := load.Avg()
	
	fmt.Fprintf(w, "%f\n", loadAvg.Load1)
}
