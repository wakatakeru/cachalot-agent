package cpu

import (
	"fmt"
	"net/http"
	"time"
	
	"github.com/shirou/gopsutil/cpu"
)

func GetUsedPercent(w http.ResponseWriter, r *http.Request) {
	v, _ := cpu.Percent(time.Duration(1*time.Second), true)
	sum := 0.0

	for i := 0; i < len(v); i++ {
		sum += v[i]
	}

	fmt.Fprintf(w, "%f\n", sum/float64(len(v)))
}
