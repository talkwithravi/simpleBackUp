package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/skip2/go-qrcode"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	port         = 8083
	discoveryPort = 5354
	storageRoot  = "MobileBackup" // will be created in user's home dir
	metadataFile = "metadata.json"
)

type MetaEntry struct {
	ID        string `json:"id"`
	FileName  string `json:"fileName"`
	Path      string `json:"path"`
	Checksum  string `json:"checksum"`
	DeviceID  string `json:"deviceId"`
	Timestamp string `json:"timestamp"`
}

var (
	pairToken string
	uploadURL string
	homeDir   string
	metaPath  string
)

func genToken(n int) string {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func localIP() string {
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			s := a.String()
			if strings.Contains(s, ".") && !strings.Contains(s, "127.0.0.1") && !strings.Contains(s, ":") {
				parts := strings.Split(s, "/")
				return parts[0]
			}
		}
	}
	return "127.0.0.1"
}

func ensureDirs() {
	homeDir, _ = os.UserHomeDir()
	storage := filepath.Join(homeDir, storageRoot)
	if _, err := os.Stat(storage); os.IsNotExist(err) {
		os.MkdirAll(storage, 0755)
	}
	metaPath = filepath.Join(storage, metadataFile)
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		os.WriteFile(metaPath, []byte("[]"), 0644)
	}
}

func appendMeta(e MetaEntry) error {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return err
	}
	var arr []MetaEntry
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	arr = append(arr, e)
	newb, _ := json.MarshalIndent(arr, "", "  ")
	return os.WriteFile(metaPath, newb, 0644)
}

func handleQR(w http.ResponseWriter, r *http.Request) {
	// Serve the QR PNG (uploadURL already set)
	png, err := qrcode.Encode(uploadURL, qrcode.Medium, 256)
	if err != nil {
		http.Error(w, "qr failure", 500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}

func handlePairInfo(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `{"upload_url":"%s"}`, uploadURL)
}

func handleList(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		http.Error(w, "no meta", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id required", 400)
		return
	}
	data, err := os.ReadFile(metaPath)
	if err != nil {
		http.Error(w, "meta read error", 500)
		return
	}
	var arr []MetaEntry
	json.Unmarshal(data, &arr)
	for _, e := range arr {
		if e.ID == id {
			http.ServeFile(w, r, e.Path)
			return
		}
	}
	http.Error(w, "not found", 404)
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	// Accept token in query or form
	token := r.FormValue("token")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token != pairToken {
		http.Error(w, "invalid token", 403)
		return
	}
	deviceId := r.FormValue("deviceId")
	if deviceId == "" {
		deviceId = "unknown-device"
	}

	err := r.ParseMultipartForm(200 << 20) // 200 MB
	if err != nil {
		http.Error(w, "parse form err", 400)
		return
	}
	file, handler, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required", 400)
		return
	}
	defer file.Close()

	now := time.Now()
	yyyy := strconv.Itoa(now.Year())
	mm := fmt.Sprintf("%02d", now.Month())
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, storageRoot, deviceId, yyyy, mm)
	os.MkdirAll(dir, 0755)

	dstPath := filepath.Join(dir, handler.Filename)
	out, err := os.Create(dstPath)
	if err != nil {
		http.Error(w, "file create failed", 500)
		return
	}
	defer out.Close()

	h := sha256.New()
	mw := io.MultiWriter(out, h)
	_, err = io.Copy(mw, file)
	if err != nil {
		http.Error(w, "write failed", 500)
		return
	}
	sum := hex.EncodeToString(h.Sum(nil))
	id := genToken(8)
	meta := MetaEntry{
		ID:        id,
		FileName:  handler.Filename,
		Path:      dstPath,
		Checksum:  sum,
		DeviceID:  deviceId,
		Timestamp: now.Format(time.RFC3339),
	}
	if err := appendMeta(meta); err != nil {
		log.Println("meta append failed:", err)
	}
	w.Write([]byte(`{"status":"ok","id":"` + id + `"}`))
}

// Simple discovery endpoint that returns server info
func handleDiscovery(w http.ResponseWriter, r *http.Request) {
	ip := localIP()
	response := map[string]string{
		"name":        "Mac Backup Agent",
		"version":     "1.0",
		"upload_url":  uploadURL,
		"server_ip":   ip,
		"server_port": strconv.Itoa(port),
		"qr_url":      fmt.Sprintf("http://%s:%d/qr", ip, port),
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Simple UDP broadcast listener for discovery requests
func startDiscoveryListener() {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: discoveryPort,
	})
	if err != nil {
		log.Printf("Discovery listener failed: %v", err)
		return
	}
	defer conn.Close()
	
	log.Printf("Discovery listener started on UDP port %d", discoveryPort)
	
	buffer := make([]byte, 1024)
	for {
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			continue
		}
		
		message := string(buffer[:n])
		if message == "DISCOVER_SIMPLEBACKUP" {
			ip := localIP()
			response := fmt.Sprintf("SIMPLEBACKUP_FOUND:%s:%d", ip, port)
			conn.WriteToUDP([]byte(response), addr)
			log.Printf("Discovery request from %s", addr.IP)
		}
	}
}

func main() {
	ensureDirs()
	pairToken = genToken(16)
	ip := localIP()
	uploadURL = fmt.Sprintf("http://%s:%d/upload?token=%s", ip, port, pairToken)

	// Start discovery listener in a goroutine
	go startDiscoveryListener()

	http.HandleFunc("/qr", handleQR)
	http.HandleFunc("/pair", handlePairInfo)
	http.HandleFunc("/upload", handleUpload)
	http.HandleFunc("/list", handleList)
	http.HandleFunc("/download", handleDownload)
	http.HandleFunc("/discovery", handleDiscovery)

	fmt.Println("🚀 Mac Agent with Simple Discovery")
	fmt.Println("==================================")
	fmt.Printf("Local: http://localhost:%d\n", port)
	fmt.Printf("Network: http://%s:%d\n", ip, port)
	fmt.Printf("QR Code: http://%s:%d/qr\n", ip, port)
	fmt.Printf("Discovery: http://%s:%d/discovery\n", ip, port)
	fmt.Println("")
	fmt.Println("🔍 Discovery Features:")
	fmt.Printf("- UDP broadcast listener on port %d\n", discoveryPort)
	fmt.Println("- Responds to 'DISCOVER_SIMPLEBACKUP' requests")
	fmt.Println("- JSON discovery endpoint at /discovery")
	fmt.Println("- Automatically discoverable on local network")
	fmt.Println("")
	fmt.Println("📱 Instructions:")
	fmt.Println("1. iPhone can discover this agent via UDP broadcast")
	fmt.Println("2. Or scan QR code: http://" + ip + ":" + strconv.Itoa(port) + "/qr")
	fmt.Println("3. Or get upload URL: http://" + ip + ":" + strconv.Itoa(port) + "/pair")
	fmt.Println("4. Or use discovery endpoint: http://" + ip + ":" + strconv.Itoa(port) + "/discovery")
	fmt.Println("")
	fmt.Println("Server running with discovery support...")
	
	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(port), nil))
}
