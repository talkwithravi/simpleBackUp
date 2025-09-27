package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	port         = 8082
	storageRoot  = "MobileBackup"
	metadataFile = "metadata.json"
)

type MetaEntry struct {
	ID        string `json:"id"`
	FileName  string `json:"fileName"`
	Path      string `json:"path"`
	Checksum  string `json:"checksum"`
	DeviceID  string `json:"deviceId"`
	Timestamp string `json:"timestamp"`
	FileSize  int64  `json:"fileSize"`
}

var metaPath string

func ensureDirs() {
	homeDir, _ := os.UserHomeDir()
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

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}

	err := r.ParseMultipartForm(200 << 20) // 200 MB
	if err != nil {
		http.Error(w, "parse error", 400)
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		http.Error(w, "no files uploaded", 400)
		return
	}

	deviceId := r.FormValue("deviceId")
	if deviceId == "" {
		deviceId = "iphone-web"
	}

	results := []string{}
	
	for _, fileHeader := range files {
		file, err := fileHeader.Open()
		if err != nil {
			results = append(results, fmt.Sprintf("Error opening %s: %v", fileHeader.Filename, err))
			continue
		}
		defer file.Close()

		now := time.Now()
		yyyy := strconv.Itoa(now.Year())
		mm := fmt.Sprintf("%02d", now.Month())
		home, _ := os.UserHomeDir()
		dir := filepath.Join(home, storageRoot, deviceId, yyyy, mm)
		os.MkdirAll(dir, 0755)

		dstPath := filepath.Join(dir, fileHeader.Filename)
		out, err := os.Create(dstPath)
		if err != nil {
			results = append(results, fmt.Sprintf("Error creating %s: %v", fileHeader.Filename, err))
			continue
		}
		defer out.Close()

		h := sha256.New()
		mw := io.MultiWriter(out, h)
		_, err = io.Copy(mw, file)
		if err != nil {
			results = append(results, fmt.Sprintf("Error writing %s: %v", fileHeader.Filename, err))
			continue
		}

		sum := hex.EncodeToString(h.Sum(nil))
		fileInfo, _ := out.Stat()
		
		meta := MetaEntry{
			ID:        fmt.Sprintf("%x", now.UnixNano())[:8],
			FileName:  fileHeader.Filename,
			Path:      dstPath,
			Checksum:  sum,
			DeviceID:  deviceId,
			Timestamp: now.Format(time.RFC3339),
			FileSize:  fileInfo.Size(),
		}
		
		if err := appendMeta(meta); err != nil {
			log.Printf("meta append failed for %s: %v", fileHeader.Filename, err)
		}

		results = append(results, fmt.Sprintf("OK: %s (%d bytes)", fileHeader.Filename, fileInfo.Size()))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"results": results,
		"count":   len(files),
	})
}

func listHandler(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		http.Error(w, "no metadata found", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func filesHandler(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		http.Error(w, "no metadata found", 500)
		return
	}
	
	var files []MetaEntry
	if err := json.Unmarshal(data, &files); err != nil {
		http.Error(w, "invalid metadata", 500)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id required", 400)
		return
	}
	
	data, err := os.ReadFile(metaPath)
	if err != nil {
		http.Error(w, "no metadata found", 500)
		return
	}
	
	var files []MetaEntry
	if err := json.Unmarshal(data, &files); err != nil {
		http.Error(w, "invalid metadata", 500)
		return
	}
	
	for _, file := range files {
		if file.ID == id {
			http.ServeFile(w, r, file.Path)
			return
		}
	}
	
	http.Error(w, "file not found", 404)
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	ip := localIP()
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Photo Backup - Web Interface</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; background: #f5f5f7; color: #1d1d1f; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { text-align: center; margin-bottom: 30px; }
        .header h1 { color: #007AFF; margin-bottom: 10px; }
        .card { background: white; border-radius: 12px; padding: 24px; margin-bottom: 20px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        .upload-area { border: 2px dashed #007AFF; border-radius: 8px; padding: 40px; text-align: center; cursor: pointer; transition: all 0.3s; }
        .upload-area:hover { background: #f0f8ff; }
        #fileInput { display: none; }
        .btn { background: #007AFF; color: white; border: none; padding: 12px 24px; border-radius: 8px; font-size: 16px; cursor: pointer; }
        .btn:disabled { background: #ccc; cursor: not-allowed; }
        .results { margin-top: 20px; }
        .info { color: #666; font-size: 14px; margin-bottom: 20px; text-align: center; }
        .qr-code { font-family: monospace; background: #f0f0f0; padding: 10px; border-radius: 4px; margin: 10px 0; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Photo Backup</h1>
            <p>Upload photos from your iPhone to Mac</p>
        </div>
        
        <div class="card">
            <div class="info">
                <strong>Server:</strong> ` + ip + `:` + strconv.Itoa(port) + `<br>
                <strong>Access this page from your iPhone Safari</strong>
            </div>
            
            <div style="text-align: center; margin: 20px 0;">
                <p>On your iPhone, open Safari and go to:</p>
                <div class="qr-code">http://` + ip + `:` + strconv.Itoa(port) + `</div>
                <p>Or scan this URL with your iPhone camera</p>
            </div>
        </div>

        <div class="card">
            <h2>Upload Photos</h2>
            <div class="upload-area" onclick="document.getElementById('fileInput').click()">
                <p>Tap to select photos</p>
                <p style="font-size: 14px; color: #666; margin-top: 10px;">Supports multiple selection</p>
            </div>
            <input type="file" id="fileInput" multiple accept="image/*,video/*" onchange="handleFileSelect(this.files)">
            
            <button class="btn" onclick="uploadFiles()" id="uploadBtn" disabled>Upload Selected Files</button>
            
            <div class="results" id="results"></div>
        </div>

        <div class="card">
            <h2>Upload History</h2>
            <button class="btn" onclick="loadHistory()">Refresh History</button>
            <div id="history" style="margin-top: 15px;"></div>
        </div>
    </div>

    <script>
        let selectedFiles = [];
        
        function handleFileSelect(files) {
            selectedFiles = Array.from(files);
            const uploadBtn = document.getElementById('uploadBtn');
            const results = document.getElementById('results');
            
            if (selectedFiles.length > 0) {
                uploadBtn.disabled = false;
                results.innerHTML = '<p>Selected ' + selectedFiles.length + ' file(s):</p><ul>' +
                    selectedFiles.map(f => '<li>' + f.name + ' (' + formatFileSize(f.size) + ')</li>').join('') +
                    '</ul>';
            } else {
                uploadBtn.disabled = true;
                results.innerHTML = '';
            }
        }
        
        function formatFileSize(bytes) {
            if (bytes === 0) return '0 Bytes';
            const k = 1024;
            const sizes = ['Bytes', 'KB', 'MB', 'GB'];
            const i = Math.floor(Math.log(bytes) / Math.log(k));
            return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
        }
        
        async function uploadFiles() {
            if (selectedFiles.length === 0) return;
            
            const uploadBtn = document.getElementById('uploadBtn');
            const results = document.getElementById('results');
            
            uploadBtn.disabled = true;
            uploadBtn.textContent = 'Uploading...';
            
            const formData = new FormData();
            selectedFiles.forEach(file => {
                formData.append('files', file);
            });
            formData.append('deviceId', 'iphone-web');
            
            try {
                const response = await fetch('/upload', {
                    method: 'POST',
                    body: formData
                });
                
                const result = await response.json();
                
                if (result.status === 'success') {
                    results.innerHTML = 
                        '<div style="color: green; font-weight: bold;">Upload successful! ' + result.count + ' file(s) uploaded</div>' +
                        '<ul>' + result.results.map(r => '<li>' + r + '</li>').join('') + '</ul>';
                    selectedFiles = [];
                    document.getElementById('fileInput').value = '';
                    uploadBtn.disabled = true;
                    loadHistory();
                } else {
                    results.innerHTML = '<div style="color: red;">Upload failed</div>';
                }
            } catch (error) {
                results.innerHTML = '<div style="color: red;">Error: ' + error.message + '</div>';
            }
            
            uploadBtn.disabled = false;
            uploadBtn.textContent = 'Upload Selected Files';
        }
        
        async function loadHistory() {
            try {
                const response = await fetch('/list');
                const history = await response.json();
                
                const historyDiv = document.getElementById('history');
                if (history.length === 0) {
                    historyDiv.innerHTML = '<p>No uploads yet</p>';
                    return;
                }
                
                historyDiv.innerHTML = 
                    '<p><strong>Total files:</strong> ' + history.length + '</p>' +
                    '<div style="max-height: 300px; overflow-y: auto;">' +
                    history.slice(-10).reverse().map(item => 
                        '<div style="border: 1px solid #ddd; padding: 10px; margin: 5px 0; border-radius: 4px;">' +
                        '<strong>' + item.fileName + '</strong><br>' +
                        '<small>' + item.timestamp + ' • ' + formatFileSize(item.fileSize) + '</small>' +
                        '</div>'
                    ).join('') +
                    '</div>';
            } catch (error) {
                document.getElementById('history').innerHTML = '<p>Error loading history</p>';
            }
        }
        
        // Load history on page load
        loadHistory();
    </script>
</body>
</html>`
	
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

func main() {
	ensureDirs()
	ip := localIP()
	
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/list", listHandler)
	
	fmt.Println("Photo Backup Web Interface")
	fmt.Println("==========================")
	fmt.Printf("Local: http://localhost:%d\n", port)
	fmt.Printf("Network: http://%s:%d\n", ip, port)
	fmt.Println("")
	fmt.Println("Instructions:")
	fmt.Println("1. Open the above URL in Safari on your iPhone")
	fmt.Println("2. Select photos to upload")
	fmt.Println("3. Files will be saved to ~/MobileBackup/")
	fmt.Println("")
	fmt.Println("Server running...")
	
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}
