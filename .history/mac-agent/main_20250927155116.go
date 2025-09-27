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
	port         = 8080
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

func main() {
	ensureDirs()
	pairToken = genToken(16)
	ip := localIP()
	uploadURL = fmt.Sprintf("http://%s:%d/upload?token=%s", ip, port, pairToken)

	http.HandleFunc("/qr", handleQR)
	http.HandleFunc("/pair", handlePairInfo)
	http.HandleFunc("/upload", handleUpload)
	http.HandleFunc("/list", handleList)
	http.HandleFunc("/download", handleDownload)

	fmt.Println("Mac Agent running.")
	fmt.Println("Open this on your Mac browser to scan QR from phone:")
	fmt.Printf("http://%s:%d/qr\n", ip, port)
	fmt.Println("Or visit /pair to get the upload url JSON:")
	fmt.Printf("http://%s:%d/pair\n", ip, port)
	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(port), nil))
}
