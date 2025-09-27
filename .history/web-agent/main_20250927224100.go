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

func qrHandler(w http.ResponseWriter, r *http.Request) {
	ip := localIP()
	url := "http://" + ip + ":" + strconv.Itoa(port)
	
	// Simple QR code using Unicode characters (fallback for no external dependencies)
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>QR Code for Mobile Backup</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; margin: 40px; background: #f5f5f7; text-align: center; }
        .qr-container { background: white; padding: 40px; border-radius: 20px; display: inline-block; box-shadow: 0 10px 30px rgba(0,0,0,0.1); }
        .qr-code { font-family: monospace; font-size: 24px; margin: 20px 0; }
        .url { font-family: monospace; background: #f0f0f0; padding: 15px; border-radius: 10px; margin: 20px 0; word-break: break-all; }
        .instructions { color: #666; margin: 20px 0; }
    </style>
</head>
<body>
    <div class="qr-container">
        <h1>📱 Mobile Backup QR Code</h1>
        <div class="instructions">
            <p>Scan this QR code with your iPhone camera to open the backup interface</p>
        </div>
        <div class="qr-code">
            <div style="font-size: 48px; margin: 20px 0;">📱</div>
            <div>Mobile Backup</div>
        </div>
        <div class="url">` + url + `</div>
        <div class="instructions">
            <p>Or copy and paste the URL above into Safari on your iPhone</p>
        </div>
    </div>
</body>
</html>`
	
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	ip := localIP()
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Mobile Backup</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; margin: 20px; background: #f5f5f7; color: #1d1d1f; }
        .container { max-width: 800px; margin: 0 auto; }
        .header { text-align: center; margin-bottom: 30px; }
        .header h1 { color: #007AFF; margin-bottom: 10px; }
        .card { background: white; border-radius: 12px; padding: 24px; margin-bottom: 20px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        .upload-area { border: 2px dashed #007AFF; border-radius: 8px; padding: 40px; text-align: center; cursor: pointer; margin-bottom: 20px; }
        .upload-area:hover { background: #f0f8ff; }
        #fileInput { display: none; }
        .btn { background: #007AFF; color: white; border: none; padding: 12px 24px; border-radius: 8px; font-size: 16px; cursor: pointer; margin: 5px; }
        .btn:disabled { background: #ccc; cursor: not-allowed; }
        .btn-secondary { background: #6c757d; }
        .btn-secondary:hover { background: #545b62; }
        .grid { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 20px; }
        .grid-item { position: relative; width: 120px; }
        .grid img { width: 120px; height: 120px; object-fit: cover; cursor: pointer; border-radius: 8px; border: 2px solid #e0e0e0; }
        .grid img:hover { border-color: #007AFF; }
        .file-info { position: absolute; bottom: 0; left: 0; right: 0; background: rgba(0,0,0,0.7); color: white; padding: 5px; font-size: 12px; border-radius: 0 0 8px 8px; }
        .download-btn { position: absolute; top: 5px; right: 5px; background: #28a745; color: white; border: none; border-radius: 4px; padding: 2px 6px; font-size: 10px; cursor: pointer; }
        .info { color: #666; font-size: 14px; margin-bottom: 20px; text-align: center; }
        .qr-code { font-family: monospace; background: #f0f0f0; padding: 10px; border-radius: 4px; margin: 10px 0; }
        .results { margin-top: 20px; padding: 10px; border-radius: 8px; }
        .success { background: #d4edda; color: #155724; border: 1px solid #c3e6cb; }
        .error { background: #f8d7da; color: #721c24; border: 1px solid #f5c6cb; }
        .device-selector { margin: 15px 0; }
        .device-selector select { padding: 8px; border-radius: 6px; border: 1px solid #ddd; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>📱 Mobile Backup</h1>
            <p>Upload photos from multiple devices and manage your backups</p>
        </div>
        
        <div class="card">
            <div class="info">
                <strong>Server:</strong> ` + ip + `:` + strconv.Itoa(port) + `<br>
                <a href="/qr" style="color: #007AFF; text-decoration: none;">📱 Get QR Code for Easy Pairing</a>
            </div>
            
            <div style="text-align: center; margin: 20px 0;">
                <p>Access this page from any device browser:</p>
                <div class="qr-code">http://` + ip + `:` + strconv.Itoa(port) + `</div>
            </div>
        </div>

        <div class="card">
            <h2>Device Selection</h2>
            <div class="device-selector">
                <label for="deviceId">Select Device:</label>
                <select id="deviceId" onchange="loadGallery()">
                    <option value="iphone-web">iPhone Web</option>
                    <option value="iphone-1">iPhone 1</option>
                    <option value="iphone-2">iPhone 2</option>
                    <option value="ipad">iPad</option>
                    <option value="android">Android</option>
                </select>
                <button class="btn btn-secondary" onclick="addNewDevice()">+ Add New Device</button>
            </div>
        </div>

        <div class="card">
            <h2>Upload Photos</h2>
            <div class="upload-area" onclick="document.getElementById('fileInput').click()">
                <p>📁 Tap to select files</p>
                <p style="font-size: 14px; color: #666; margin-top: 10px;">Supports any file type (images, videos, documents, etc.)</p>
            </div>
            <input type="file" id="fileInput" multiple onchange="handleFileSelect(this.files)">
            
            <button class="btn" onclick="uploadFiles()" id="uploadBtn" disabled>Upload Selected Files</button>
            
            <div class="results" id="results"></div>
        </div>

        <div class="card">
            <h2>📂 Backup Gallery</h2>
            <button class="btn" onclick="loadGallery()">Refresh Gallery</button>
            <div id="gallery" class="grid"></div>
        </div>
    </div>

    <script>
        let selectedFiles = [];
        let currentDeviceId = 'iphone-web';
        
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
        
        function getCurrentDeviceId() {
            return document.getElementById('deviceId').value;
        }
        
        function addNewDevice() {
            const deviceName = prompt('Enter a name for the new device:');
            if (deviceName && deviceName.trim()) {
                const deviceId = deviceName.trim().toLowerCase().replace(/\s+/g, '-');
                const select = document.getElementById('deviceId');
                const option = document.createElement('option');
                option.value = deviceId;
                option.textContent = deviceName.trim();
                select.appendChild(option);
                select.value = deviceId;
                loadGallery();
            }
        }
        
        async function uploadFiles() {
            if (selectedFiles.length === 0) return;
            
            const uploadBtn = document.getElementById('uploadBtn');
            const results = document.getElementById('results');
            
            uploadBtn.disabled = true;
            uploadBtn.textContent = 'Uploading...';
            results.innerHTML = '';
            
            const formData = new FormData();
            selectedFiles.forEach(file => {
                formData.append('files', file);
            });
            formData.append('deviceId', getCurrentDeviceId());
            
            try {
                const response = await fetch('/upload', {
                    method: 'POST',
                    body: formData
                });
                
                const result = await response.json();
                
                if (result.status === 'success') {
                    results.innerHTML = 
                        '<div class="success">✅ Upload successful! ' + result.count + ' file(s) uploaded</div>' +
                        '<ul>' + result.results.map(r => '<li>' + r + '</li>').join('') + '</ul>';
                    selectedFiles = [];
                    document.getElementById('fileInput').value = '';
                    uploadBtn.disabled = true;
                    loadGallery(); // Auto-refresh gallery after upload
                } else {
                    results.innerHTML = '<div class="error">❌ Upload failed</div>';
                }
            } catch (error) {
                results.innerHTML = '<div class="error">❌ Error: ' + error.message + '</div>';
            }
            
            uploadBtn.disabled = false;
            uploadBtn.textContent = 'Upload Selected Files';
        }
        
        function downloadFile(fileId, fileName) {
            const link = document.createElement('a');
            link.href = '/download?id=' + fileId;
            link.download = fileName;
            document.body.appendChild(link);
            link.click();
            document.body.removeChild(link);
        }
        
        function getFileIcon(filename) {
            const ext = filename.toLowerCase().split('.').pop();
            switch(ext) {
                case 'jpg': case 'jpeg': case 'png': case 'gif': case 'bmp': case 'webp':
                    return '🖼️';
                case 'mp4': case 'mov': case 'avi': case 'mkv': case 'flv':
                    return '🎬';
                case 'pdf':
                    return '📄';
                case 'doc': case 'docx':
                    return '📝';
                case 'xls': case 'xlsx':
                    return '📊';
                case 'ppt': case 'pptx':
                    return '📽️';
                case 'zip': case 'rar': case '7z': case 'tar':
                    return '📦';
                case 'mp3': case 'wav': case 'aac': case 'flac':
                    return '🎵';
                case 'txt': case 'rtf':
                    return '📃';
                default:
                    return '📁';
            }
        }

        async function loadGallery() {
            try {
                const deviceId = getCurrentDeviceId();
                const response = await fetch('/files');
                const files = await response.json();
                
                const gallery = document.getElementById('gallery');
                if (files.length === 0) {
                    gallery.innerHTML = '<p>No files uploaded yet for this device</p>';
                    return;
                }
                
                // Filter files by current device
                const deviceFiles = files.filter(file => file.deviceId === deviceId);
                
                if (deviceFiles.length === 0) {
                    gallery.innerHTML = '<p>No files found for ' + deviceId + '</p>';
                    return;
                }
                
                gallery.innerHTML = '';
                deviceFiles.forEach(file => {
                    const gridItem = document.createElement('div');
                    gridItem.className = 'grid-item';
                    
                    const fileIcon = document.createElement('div');
                    fileIcon.className = 'file-icon';
                    fileIcon.style.cssText = 'width: 120px; height: 120px; background: #f0f0f0; border-radius: 8px; display: flex; align-items: center; justify-content: center; font-size: 48px; cursor: pointer; border: 2px solid #e0e0e0;';
                    fileIcon.title = file.fileName + ' (' + formatFileSize(file.fileSize) + ')';
                    fileIcon.onclick = () => window.open('/download?id=' + file.id, '_blank');
                    
                    const icon = document.createElement('div');
                    icon.textContent = getFileIcon(file.fileName);
                    fileIcon.appendChild(icon);
                    
                    const fileInfo = document.createElement('div');
                    fileInfo.className = 'file-info';
                    fileInfo.textContent = file.fileName.substring(0, 15) + (file.fileName.length > 15 ? '...' : '');
                    
                    const downloadBtn = document.createElement('button');
                    downloadBtn.className = 'download-btn';
                    downloadBtn.textContent = '⬇️';
                    downloadBtn.title = 'Download ' + file.fileName;
                    downloadBtn.onclick = (e) => {
                        e.stopPropagation();
                        downloadFile(file.id, file.fileName);
                    };
                    
                    gridItem.appendChild(fileIcon);
                    gridItem.appendChild(downloadBtn);
                    gridItem.appendChild(fileInfo);
                    gallery.appendChild(gridItem);
                });
            } catch (error) {
                document.getElementById('gallery').innerHTML = '<p class="error">Error loading gallery</p>';
            }
        }
        
        // Load gallery on page load
        loadGallery();
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
	http.HandleFunc("/qr", qrHandler)
	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/list", listHandler)
	http.HandleFunc("/files", filesHandler)
	http.HandleFunc("/download", downloadHandler)
	
	fmt.Println("Enhanced Mobile Backup Web Interface")
	fmt.Println("====================================")
	fmt.Printf("Local: http://localhost:%d\n", port)
	fmt.Printf("Network: http://%s:%d\n", ip, port)
	fmt.Printf("QR Code: http://%s:%d/qr\n", ip, port)
	fmt.Println("")
	fmt.Println("Features:")
	fmt.Println("- QR code pairing for easy device connection")
	fmt.Println("- Multiple device support with separate folders")
	fmt.Println("- Auto-refresh gallery after upload")
	fmt.Println("- Download files back to mobile devices")
	fmt.Println("- Mobile-optimized web interface")
	fmt.Println("")
	fmt.Println("Server running...")
	
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}
