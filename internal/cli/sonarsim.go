package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/r0n1n9/netwatch/internal/sonar"
)

var (
	simTo    string
	simNodes int
)

var sonarSimCmd = &cobra.Command{
	Use:   "sonar-sim",
	Short: "SONAR-SIM — send simulated CSI frames to a running console (no hardware needed)",
	Long:  "Streams synthetic ESP32-style CSI frames over UDP so you can try SONAR before the\nhardware arrives. Start the console with --sonar-udp first, then point this at it.",
	RunE: func(cmd *cobra.Command, args []string) error {
		conn, err := net.Dial("udp", simTo)
		if err != nil {
			return err
		}
		defer conn.Close()
		var sims []*sonar.Sim
		for i := 0; i < simNodes; i++ {
			sims = append(sims, sonar.NewSim(fmt.Sprintf("sim-%c", 'A'+i), int64(i+1)))
		}
		fmt.Printf("sending %d simulated nodes to udp %s — ctrl-c to stop\n", simNodes, simTo)
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt)
		t := time.NewTicker(40 * time.Millisecond)
		defer t.Stop()
		start := time.Now()
		for {
			select {
			case <-stop:
				return nil
			case <-t.C:
			}
			phase := math.Mod(time.Since(start).Seconds(), 20)
			for i, sim := range sims {
				motion := 0.0
				if lo := 6 + float64(i)*4; phase > lo && phase < lo+4 {
					motion = 1
				}
				b, _ := json.Marshal(sim.Frame(motion))
				if _, err := conn.Write(b); err != nil {
					return err
				}
			}
		}
	},
}

func init() {
	sonarSimCmd.Flags().StringVar(&simTo, "to", "127.0.0.1:5566", "UDP address of the console's --sonar-udp listener")
	sonarSimCmd.Flags().IntVar(&simNodes, "nodes", 3, "number of simulated nodes")
	rootCmd.AddCommand(sonarSimCmd)
}
