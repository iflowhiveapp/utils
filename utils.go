// utils.go
package utils

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Knetic/govaluate"
)

/* ---------- Metrics configuration ---------- */

// MetricsConfig describes how a metric should be converted.
type MetricsConfig struct {
	ShortCode   string `json:"shortCode"`
	Description string `json:"description"`
	Formula     string `json:"formula"` // e.g. "(#VALUE / #TOTAL_VALUE) * 100"
}

// Metrics is a single metric entry in the global list.
type Metrics struct {
	Metrics       string        `json:"metrics"`  // CPU, Memory, Disk, …
	BaseUnit      string        `json:"baseUnit"` // Bytes, Percentage, …
	MetricsConfig MetricsConfig `json:"metricsConfig"`
}

/* ---------- Generic converters ---------- */

// StringToUint64 converts "123" or "123.45" ➜ 123.
func StringToUint64(str string) uint64 {
	if strings.Contains(str, ".") {
		str = strings.Split(str, ".")[0]
	}
	v, _ := strconv.ParseUint(str, 10, 64) // ignore error, returns 0 on failure
	return v
}

// ConvertStringToInt converts "42" ➜ 42.
func ConvertStringToInt(str string) int {
	v, _ := strconv.Atoi(str)
	return v
}

// StringToFloat converts "3.14" ➜ 3.14.
func StringToFloat(str string) float64 {
	v, _ := strconv.ParseFloat(str, 64)
	return v
}

/* ---------- Metric‑specific helpers ---------- */

// ValueConvertPercentage converts used vs total cores ➜ percentage (%).
func ValueConvertPercentage(config []Metrics, usedCores, totalCores float64) (float64, error) {
	for _, m := range config {
		if m.Metrics == "CPU" && m.BaseUnit == "Percentage" {
			return applyFormula(m.MetricsConfig.Formula, usedCores, totalCores)
		}
	}
	return 0, fmt.Errorf("CPU metrics configuration not found")
}

// MemoryValueConvert converts bytes ➜ MiB/GiB/… depending on config.
func MemoryValueConvert(config []Metrics, used, total, free uint64) (u, t, f float64, unit string, err error) {
	return genericMemoryConvert(config, "Memory", used, total, free)
}

// DiskValueConvert converts bytes ➜ MiB/GiB/… depending on config.
func DiskValueConvert(config []Metrics, used, total, free uint64) (u, t, f float64, unit string, err error) {
	return genericMemoryConvert(config, "Disk", used, total, free)
}

// genericMemoryConvert is shared by Memory/Disk helpers.
func genericMemoryConvert(config []Metrics, metric string, used, total, free uint64) (float64, float64, float64, string, error) {
	for _, m := range config {
		if m.Metrics == metric && m.MetricsConfig.Formula != "" && m.BaseUnit != "Bytes" {
			u, err1 := applyFormula(m.MetricsConfig.Formula, float64(used), float64(total))
			t, err2 := applyFormula(m.MetricsConfig.Formula, float64(total), float64(total))
			f, err3 := applyFormula(m.MetricsConfig.Formula, float64(free), float64(total))
			if err := firstErr(err1, err2, err3); err != nil {
				return 0, 0, 0, m.BaseUnit, err
			}
			return RoundTwo(u), RoundTwo(t), RoundTwo(f), m.BaseUnit, nil
		}
	}
	// Fallback: leave values in Bytes.
	return RoundTwo(float64(used)), RoundTwo(float64(total)), RoundTwo(float64(free)), "Bytes", nil
}

/* ---------- Internal helpers ---------- */

func applyFormula(formula string, value, total float64) (float64, error) {
	formula = strings.ReplaceAll(formula, "#VALUE", strconv.FormatFloat(value, 'f', 2, 64))
	formula = strings.ReplaceAll(formula, "#TOTAL_VALUE", strconv.FormatFloat(total, 'f', 2, 64))
	return evalFormula(formula)
}

func evalFormula(exprStr string) (float64, error) {
	expr, err := govaluate.NewEvaluableExpression(exprStr)
	if err != nil {
		return 0, err
	}
	v, err := expr.Evaluate(nil)
	if err != nil {
		return 0, err
	}
	return RoundTwo(v.(float64)), nil
}

func RoundTwo(v float64) float64 { return math.Round(v*100) / 100 }

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func GetSystemTimeZone() (string, error) {
	if runtime.GOOS == "windows" {
		// On Windows, use the time package to get the time zone
		_, offset := time.Now().Zone()
		return fmt.Sprintf("UTC%+d", offset/3600), nil
	} else {
		// On Linux, use the timedatectl command
		out, err := exec.Command("timedatectl").Output()
		if err != nil {
			return "", err
		}

		// Parse the output of timedatectl for "Time zone"
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			if strings.Contains(line, "Time zone:") {
				parts := strings.Fields(line)
				if len(parts) > 2 {
					return parts[2], nil // Return the time zone ID
				}
			}
		}

		return "", fmt.Errorf("time zone not found")
	}

}

// StructToJSONString converts a struct to a JSON string.
func StructToJSONString(data interface{}) string {
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Printf("Error converting struct to JSON: %v", err)
		return ""
	}
	return string(jsonData)
}

func GetHostName() string {
	hostName, err := os.Hostname()
	if err != nil {
		return ""
	}
	return hostName
}

func MacAddresses() []string {
	var macList []string
	interfaces, err := net.Interfaces()
	if err != nil {
		fmt.Printf("Error fetching interfaces: %v\n", err)
		return nil
	}
	for _, iface := range interfaces {
		if iface.HardwareAddr.String() != "" {
			macList = append(macList, iface.HardwareAddr.String())
		}
	}
	return macList
}

func ParseMemoryString(memStr string) (uint64, error) {
	memStr = strings.TrimSpace(memStr)
	if len(memStr) < 2 {
		return 0, fmt.Errorf("invalid memory string: %s", memStr)
	}

	// Find the position where the unit starts
	var unitStart int
	for i, r := range memStr {
		if (r < '0' || r > '9') && r != '.' {
			unitStart = i
			break
		}
	}

	if unitStart == 0 {
		return 0, fmt.Errorf("invalid memory string: %s", memStr)
	}

	valueStr := memStr[:unitStart]
	unit := memStr[unitStart:]

	value, err := strconv.ParseFloat(strings.TrimSpace(valueStr), 64)
	if err != nil {
		return 0, fmt.Errorf("error parsing memory value: %v", err)
	}

	// Convert based on the unit
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "B":
		return uint64(value), nil
	case "KIB":
		return uint64(value * 1024), nil
	case "MIB":
		return uint64(value * 1024 * 1024), nil
	case "GIB":
		return uint64(value * 1024 * 1024 * 1024), nil
	case "TIB":
		return uint64(value * 1024 * 1024 * 1024 * 1024), nil
	default:
		return 0, fmt.Errorf("unknown memory unit: %s", unit)
	}
}

func InternetProtocolList() []string {
	var ipList []string
	interfaces, err := net.Interfaces()
	if err != nil {
		fmt.Printf("Error fetching interfaces: %v\n", err)
		return nil
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			fmt.Printf("Error fetching addresses for interface %s: %v\n", iface.Name, err)
			continue
		}
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil {
				fmt.Printf("Error parsing address for interface %s: %v\n", iface.Name, err)
				continue
			}
			ipList = append(ipList, ip.String())
		}
	}
	return ipList
}

func GetContainerIDByName(containerName string) (bool, string, error) {
	// Run the docker ps command to get container IDs and their names
	cmd := exec.Command("sh", "-c", "docker ps --format '{{.ID}} {{.Names}}'")
	out, err := cmd.Output()
	if err != nil {
		return false, "", fmt.Errorf("error running docker ps: %w", err)
	}

	// Process the output line by line
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue // Skip invalid lines
		}

		containerID := parts[0]
		name := parts[1]

		// Check if the name matches the container name
		if name == containerName {
			log.Printf("containerId: %s", containerID)
			return true, containerID, nil
		}
	}

	// No container found with the specified name
	return false, "", nil
}

func CheckProcesses() map[string]string {
	result := make(map[string]string)
	if runtime.GOOS == "windows" {
		// On Windows, use netstat -ano and tasklist
		out, err := exec.Command("netstat", "-ano").Output()
		if err != nil {
			return result
		}
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 5 && (fields[0] == "TCP" || fields[0] == "UDP") {
				pid := fields[len(fields)-1]
				// Get process name by PID
				taskOut, err := exec.Command("tasklist", "/FI", "PID eq "+pid).Output()
				if err == nil {
					taskLines := strings.Split(string(taskOut), "\n")
					for _, tline := range taskLines {
						if strings.Contains(tline, pid) {
							procFields := strings.Fields(tline)
							if len(procFields) > 0 {
								result[pid] = procFields[0]
							}
						}
					}
				}
			}
		}
		return result
	}
	// Linux logic (existing)
	processes := []string{"elasticsearch", "neo4j", "kafka"}
	for _, name := range processes {
		cmd := fmt.Sprintf(`ps -ef | grep '%s' | awk '{print $2}' | while read pid; do
			port=$(netstat -tulpn | grep $pid | awk '{print $4}' | cut -d: -f2);
			if [ ! -z "$port" ]; then
				echo "$pid";
				break;
			fi;
		done`, name)
		out, err := exec.Command("bash", "-c", cmd).Output()
		if err != nil {
			return nil
		}
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			pid := strings.TrimSpace(line)
			result[pid] = name
			break
		}
	}
	return result
}

func GetJarName() map[string]string {
	pidToJarMap := make(map[string]string)
	if runtime.GOOS == "windows" {
		// Use wmic to get Java processes and their command lines
		out, err := exec.Command("wmic", "process", "where", "caption='java.exe'", "get", "ProcessId,CommandLine", "/FORMAT:LIST").Output()
		if err != nil {
			fmt.Println("Error executing wmic command:", err)
			return pidToJarMap
		}
		blocks := strings.Split(string(out), "\n\n")
		for _, block := range blocks {
			lines := strings.Split(block, "\n")
			var pid, cmdline string
			for _, line := range lines {
				if strings.HasPrefix(line, "ProcessId=") {
					pid = strings.TrimPrefix(line, "ProcessId=")
				}
				if strings.HasPrefix(line, "CommandLine=") {
					cmdline = strings.TrimPrefix(line, "CommandLine=")
				}
			}
			if pid != "" && cmdline != "" {
				// Find .jar in command line
				for _, part := range strings.Fields(cmdline) {
					if strings.HasSuffix(part, ".jar") {
						pidToJarMap[pid] = part
					}
				}
			}
		}
		return pidToJarMap
	}
	// Linux logic (existing)
	cmd := exec.Command("bash", "-c", `
ps aux | grep java | awk -F ' ' '{for(i=11;i<=NF;i++) if($i ~ /\\.jar/) print $i}' | sed 's#.*/##' | while read jar; do
    pid=$(pgrep -f $jar)
    echo "$pid $jar"
done`)
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Error executing command:", err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			pid := parts[0]
			jar := strings.Join(parts[1:], " ")
			pidToJarMap[pid] = jar
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading command output:", err)
	}
	return pidToJarMap
}

func GetIPAddresses() map[string]string {
	ipAddresses := make(map[string]string)
	allInterfaces, err := net.Interfaces()
	if err != nil {
		fmt.Println(err)
		return ipAddresses
	}
	for _, iface := range allInterfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			fmt.Println(err)
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					ipAddresses[iface.Name] = ipnet.IP.String()
					break
				}
			}
		}
	}
	return ipAddresses
}

func GetPIDByPort(port string) (int32, string, error) {
	if runtime.GOOS == "windows" {
		// Windows logic: netstat -ano
		out, err := exec.Command("netstat", "-ano").Output()
		if err != nil {
			return -1, "", fmt.Errorf("error running netstat command: %w", err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 5 && (fields[0] == "TCP" || fields[0] == "UDP") {
				if strings.HasSuffix(fields[1], ":"+port) {
					pidStr := fields[len(fields)-1]
					pidNum, err := strconv.ParseInt(pidStr, 10, 32)
					if err != nil {
						return -1, "", fmt.Errorf("error parsing PID: %w", err)
					}
					return int32(pidNum), "nil", nil
				}
			}
		}
		return -1, "", errors.New("no process found on the given port (Windows)")
	}

	// Linux logic: first check if anything is listening at all
	ln, err := net.Listen("tcp", ":"+port)
	if err == nil {
		ln.Close()
		return -1, "", errors.New("no process found on the given port")
	}

	// Run `ss` to extract PID(s)
	ssCmd := fmt.Sprintf(
		"ss -tlnp | grep ':%s ' | awk '{print $6}' | cut -d',' -f2 | cut -d'=' -f2",
		port,
	)
	cmd := exec.Command("sh", "-c", ssCmd)
	out, err := cmd.Output()
	if err != nil {
		return -1, "", fmt.Errorf("error running ss command: %w", err)
	}

	// Split on whitespace/newlines and take the first non-empty entry
	lines := strings.Fields(strings.TrimSpace(string(out)))
	if len(lines) == 0 {
		// Docker fallback: look for a container publishing this port
		dockerPS := fmt.Sprintf("docker ps --filter publish=%s --format {{.ID}}", port)
		psCmd := exec.Command("sh", "-c", dockerPS)
		psOut, err := psCmd.Output()
		if err != nil || len(psOut) == 0 {
			return -1, "", errors.New("no process found on the given port")
		}
		containerID := strings.TrimSpace(string(psOut))

		// Inspect the container's PID
		inspectCmd := exec.Command(
			"docker", "inspect", "--format", "{{.State.Pid}}", containerID,
		)
		inspOut, err := inspectCmd.Output()
		if err != nil || len(inspOut) == 0 {
			return -1, "", errors.New("docker inspect failed to return PID")
		}
		lines = strings.Fields(strings.TrimSpace(string(inspOut)))
		if len(lines) == 0 {
			return -1, "", errors.New("no PID found in Docker inspect output")
		}
	}

	pidStr := lines[0]
	pidNum, err := strconv.ParseInt(pidStr, 10, 32)
	if err != nil {
		return -1, "", fmt.Errorf("error parsing PID: %w", err)
	}
	return int32(pidNum), "nil", nil
}
