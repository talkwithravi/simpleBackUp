import SwiftUI
import PhotosUI

struct ContentView: View {
    @State private var showScanner = false
    @State private var serverURL: String? = nil
    @State private var showPicker = false
    @State private var selectedItems: [PHPickerResult] = []
    @State private var statusText: String = "Not paired"

    var body: some View {
        VStack(spacing:20) {
            Text("SnapVault (demo)").font(.title)
            Text(statusText).foregroundColor(.gray)

            Button("Scan Pairing QR") {
                showScanner.toggle()
            }
            .sheet(isPresented: $showScanner) {
                QRScannerView { code in
                    DispatchQueue.main.async {
                        serverURL = code
                        statusText = "Paired: \(code)"
                        showScanner = false
                    }
                }
            }

            Button("Pick Photos to Upload") {
                showPicker = true
            }.disabled(serverURL == nil)
            .sheet(isPresented:$showPicker) {
                PhotoPicker(configuration: makePickerConfig(), onComplete: { results in
                    selectedItems = results
                    uploadSelected()
                    showPicker = false
                })
            }

            Spacer()
        }.padding()
    }

    func makePickerConfig() -> PHPickerConfiguration {
        var cfg = PHPickerConfiguration()
        cfg.selectionLimit = 0 // multiple
        cfg.filter = .images
        return cfg
    }

    func uploadSelected() {
        guard let server = serverURL else { return }
        // PHPickerResult -> get Data via itemProvider
        for item in selectedItems {
            if item.itemProvider.canLoadObject(ofClass: UIImage.self) {
                item.itemProvider.loadFileRepresentation(forTypeIdentifier: "public.image") { url, err in
                    if let url = url {
                        // Copy temp file and upload
                        let fname = url.lastPathComponent
                        let tmp = FileManager.default.temporaryDirectory.appendingPathComponent(fname)
                        try? FileManager.default.removeItem(at: tmp)
                        try? FileManager.default.copyItem(at: url, to: tmp)
                        uploadFile(fileURL: tmp, server: server, deviceId: UIDevice.current.identifierForVendor?.uuidString ?? "ios")
                    }
                }
            }
        }
    }

    func uploadFile(fileURL: URL, server: String, deviceId: String) {
        // server is expected to be the upload URL like http://<ip>:8080/upload?token=...
        guard let url = URL(string: server) else { return }
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        // Build multipart/form-data
        let boundary = "Boundary-\(UUID().uuidString)"
        request.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")

        var body = Data()
        // token may already be in query; send deviceId and file
        body.appendString("--\(boundary)\r\n")
        body.appendString("Content-Disposition: form-data; name=\"deviceId\"\r\n\r\n")
        body.appendString("\(deviceId)\r\n")

        let data = try? Data(contentsOf: fileURL)
        let filename = fileURL.lastPathComponent
        let mime = "image/jpeg" // approximate

        body.appendString("--\(boundary)\r\n")
        body.appendString("Content-Disposition: form-data; name=\"file\"; filename=\"\(filename)\"\r\n")
        body.appendString("Content-Type: \(mime)\r\n\r\n")
        body.append(data ?? Data())
        body.appendString("\r\n")
        body.appendString("--\(boundary)--\r\n")

        request.httpBody = body

        let task = URLSession.shared.dataTask(with: request) { data, resp, err in
            if let e = err { print("upload err", e); return }
            if let d = data, let s = String(data: d, encoding: .utf8) {
                print("upload resp:", s)
                DispatchQueue.main.async {
                    statusText = "Uploaded \(filename)"
                }
            }
        }
        task.resume()
    }
}

// small helpers
extension Data {
    mutating func appendString(_ s: String) {
        if let d = s.data(using: .utf8) { append(d) }
    }
}

// A wrapper for PHPicker to return results
struct PhotoPicker: UIViewControllerRepresentable {
    var configuration: PHPickerConfiguration
    var onComplete: ([PHPickerResult])->Void

    func makeUIViewController(context: Context) -> PHPickerViewController {
        let picker = PHPickerViewController(configuration: configuration)
        picker.delegate = context.coordinator
        return picker
    }
    func updateUIViewController(_ uiViewController: PHPickerViewController, context: Context) {}
    func makeCoordinator() -> Coordinator { Coordinator(onComplete) }
    class Coordinator: NSObject, PHPickerViewControllerDelegate {
        var onComplete: ([PHPickerResult])->Void
        init(_ cb: @escaping ([PHPickerResult])->Void) { onComplete = cb }
        func picker(_ picker: PHPickerViewController, didFinishPicking results: [PHPickerResult]) {
            picker.dismiss(animated: true)
            onComplete(results)
        }
    }
}
