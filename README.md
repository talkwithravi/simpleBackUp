# Simple Photo Backup System

A local Wi-Fi photo backup system that allows an iPhone to send photos to a Mac over the same network.

## Components

### 1. Mac Agent (Go)
- Serves QR codes for pairing
- Handles file uploads via HTTP
- Stores photos in `~/MobileBackup/{deviceId}/{yyyy}/{mm}/`
- Maintains metadata in `~/MobileBackup/metadata.json`

### 2. iPhone App (SwiftUI)
- Scans QR codes for pairing
- Selects photos from library
- Uploads photos to Mac agent

## Setup Instructions

### Mac Agent Setup

1. **Install Go 1.21+** if not already installed
2. **Navigate to mac-agent directory**:
   ```bash
   cd mac-agent
   ```
3. **Install dependencies**:
   ```bash
   go mod tidy
   ```
4. **Run the server**:
   ```bash
   go run main.go
   ```
5. **Access the QR code**:
   - Open your Mac browser to `http://<your-mac-ip>:8080/qr`
   - Or get the upload URL directly at `http://<your-mac-ip>:8080/pair`

### iPhone App Setup

1. **Create a new Xcode project** (Single View App or SwiftUI)
2. **Add the provided Swift files** to your project:
   - `QRCodeScanner.swift`
   - `ContentView.swift`
3. **Configure Info.plist** with the provided permissions
4. **Build and run** on a real iPhone device (camera required for QR scanning)

## Testing the System

### Step 1: Start Mac Agent
```bash
cd mac-agent
go run main.go
```

### Step 2: Access QR Code
- Open `http://<your-mac-ip>:8080/qr` in your Mac browser
- Keep the QR code visible on screen

### Step 3: Pair iPhone
- Open the iPhone app
- Tap "Scan Pairing QR"
- Scan the QR code displayed on your Mac
- The app will store the upload URL

### Step 4: Upload Photos
- Tap "Pick Photos to Upload"
- Select one or more photos
- The app will upload them to your Mac

### Step 5: Verify Upload
- Check `~/MobileBackup/{deviceId}/{yyyy}/{mm}/` for uploaded files
- Check `~/MobileBackup/metadata.json` for upload records

## API Endpoints

- `GET /qr` - Returns QR code PNG for pairing
- `GET /pair` - Returns JSON with upload URL
- `POST /upload` - Accepts photo uploads (multipart/form-data)
- `GET /list` - Returns metadata JSON
- `GET /download?id={id}` - Downloads specific file

## Development Notes

- **HTTP Only**: This version uses HTTP for simplicity during development
- **ATS Exception**: iOS app allows arbitrary loads for local network testing
- **Local Network**: Both devices must be on the same Wi-Fi network
- **Production**: For production use, implement TLS with proper certificates

## File Structure

```
simpleBackup/
├── mac-agent/
│   ├── go.mod
│   └── main.go
├── ios-app/
│   ├── Info.plist
│   ├── QRCodeScanner.swift
│   └── ContentView.swift
└── README.md
```

## Troubleshooting

1. **QR code not scanning**: Ensure both devices are on the same network
2. **Upload fails**: Check Mac firewall settings and ensure port 8080 is accessible
3. **Permissions denied**: Verify Info.plist contains required privacy descriptions
4. **File not found**: Check that the Mac agent is running and accessible via IP address

## Security Considerations

- This is a development version using HTTP
- For production, implement HTTPS with proper certificates
- Consider adding authentication beyond the one-time token
- Implement file encryption for sensitive photos
