import Foundation

/// The bytes of `files.upload` and `files.download` (client/ui/more/files/host.ts): one
/// URLSession whose delegate runs on its own serial queue. An upload streams from the
/// picked file (`uploadTask(with:fromFile:)`); a download streams into a temp file a
/// chunk at a time, held to `maxBytes`, then is moved into the chosen folder.
final class Transfers: NSObject, URLSessionDataDelegate, @unchecked Sendable {
  struct Answer { let status: Int; let body: String }
  enum Outcome { case upload(Answer), download(bytes: Int64, file: URL) }

  private final class Job {
    let id: String
    let upload: Bool
    let maxBytes: Int64
    let finish: (Result<Outcome, Error>) -> Void
    var body = Data()
    var status = 0
    var file: URL?
    var handle: FileHandle?
    var written: Int64 = 0
    var reportedAt = Date.distantPast
    var cancelled = false
    var failure: String?
    init(id: String, upload: Bool, maxBytes: Int64, finish: @escaping (Result<Outcome, Error>) -> Void) {
      self.id = id
      self.upload = upload
      self.maxBytes = maxBytes
      self.finish = finish
    }
  }

  /// `(uploadId, bytesSent, totalBytes)`, at most every 200 ms and once at the end; -1 when the total is unknown.
  var onProgress: (@Sendable (String, Int64, Int64) -> Void)?
  private let lock = NSLock()
  private var jobs: [Int: Job] = [:]
  private var tasks: [String: URLSessionTask] = [:]
  private lazy var session: URLSession = {
    let queue = OperationQueue()
    queue.maxConcurrentOperationCount = 1
    let configuration = URLSessionConfiguration.ephemeral
    configuration.timeoutIntervalForRequest = 60
    configuration.urlCache = nil
    return URLSession(configuration: configuration, delegate: self, delegateQueue: queue)
  }()

  func upload(id: String, request: URLRequest, file: URL, finish: @escaping (Result<Outcome, Error>) -> Void) throws {
    try start(id: id, task: session.uploadTask(with: request, fromFile: file), job: Job(id: id, upload: true, maxBytes: .max, finish: finish))
  }

  func download(id: String, request: URLRequest, maxBytes: Int64, finish: @escaping (Result<Outcome, Error>) -> Void) throws {
    try start(id: id, task: session.dataTask(with: request), job: Job(id: id, upload: false, maxBytes: maxBytes, finish: finish))
  }

  /// Whether there was such a transfer to stop.
  func cancel(id: String) -> Bool {
    lock.lock()
    let task = tasks[id]
    if let task { jobs[task.taskIdentifier]?.cancelled = true }
    lock.unlock()
    task?.cancel()
    return task != nil
  }

  private func start(id: String, task: URLSessionTask, job: Job) throws {
    lock.lock()
    defer { lock.unlock() }
    guard tasks[id] == nil else { throw FilesError("A transfer with this ID is already active.") }
    tasks[id] = task
    jobs[task.taskIdentifier] = job
    task.resume()
  }

  private func job(_ task: URLSessionTask) -> Job? {
    lock.lock()
    defer { lock.unlock() }
    return jobs[task.taskIdentifier]
  }

  // Redirects are refused: a capability or signature is for one URL.
  func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse, newRequest request: URLRequest, completionHandler: @escaping (URLRequest?) -> Void) {
    completionHandler(nil)
  }

  func urlSession(_ session: URLSession, task: URLSessionTask, didSendBodyData bytesSent: Int64, totalBytesSent: Int64, totalBytesExpectedToSend: Int64) {
    guard let job = job(task), job.upload, !job.cancelled else { return }
    let now = Date()
    let done = totalBytesExpectedToSend > 0 && totalBytesSent >= totalBytesExpectedToSend
    guard done || now.timeIntervalSince(job.reportedAt) >= 0.2 else { return }
    job.reportedAt = now
    onProgress?(job.id, totalBytesSent, totalBytesExpectedToSend > 0 ? totalBytesExpectedToSend : -1)
  }

  func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive response: URLResponse, completionHandler: @escaping (URLSession.ResponseDisposition) -> Void) {
    guard let job = job(dataTask) else { return completionHandler(.cancel) }
    job.status = (response as? HTTPURLResponse)?.statusCode ?? 0
    if job.upload { return completionHandler(.allow) }
    guard (200..<300).contains(job.status) else {
      job.failure = "Session file download failed (HTTP \(job.status))."
      return completionHandler(.cancel)
    }
    if response.expectedContentLength > job.maxBytes {
      job.failure = "Session file download exceeded the \(job.maxBytes) byte limit."
      return completionHandler(.cancel)
    }
    let file = FileManager.default.temporaryDirectory.appendingPathComponent("mewla-download-\(UUID().uuidString)")
    guard FileManager.default.createFile(atPath: file.path, contents: nil), let handle = try? FileHandle(forWritingTo: file) else {
      job.failure = "Could not open the selected download destination."
      return completionHandler(.cancel)
    }
    job.file = file
    job.handle = handle
    completionHandler(.allow)
  }

  func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data) {
    guard let job = job(dataTask) else { return }
    if job.upload {
      if job.body.count < 1 << 20 { job.body.append(data) }
      return
    }
    job.written += Int64(data.count)
    if job.written > job.maxBytes {
      job.failure = "Session file download exceeded the \(job.maxBytes) byte limit."
      return dataTask.cancel()
    }
    do {
      try job.handle?.write(contentsOf: data)
    } catch {
      job.failure = "Could not write the download."
      dataTask.cancel()
    }
  }

  func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
    lock.lock()
    let job = jobs.removeValue(forKey: task.taskIdentifier)
    if let job { tasks.removeValue(forKey: job.id) }
    lock.unlock()
    guard let job else { return }
    try? job.handle?.close()
    let fail = { (message: String) in
      if let file = job.file { try? FileManager.default.removeItem(at: file) }
      job.finish(.failure(FilesError(message)))
    }
    if job.cancelled { return fail(job.upload ? "Attachment upload cancelled." : "Session file download was cancelled.") }
    if let failure = job.failure { return fail(failure) }
    if error != nil { return fail(job.upload ? "Could not upload the attachment." : "Session file download failed.") }
    if job.upload {
      job.finish(.success(.upload(Answer(status: job.status, body: String(decoding: job.body, as: UTF8.self)))))
    } else if let file = job.file {
      job.finish(.success(.download(bytes: job.written, file: file)))
    } else {
      fail("Session file download returned an empty body.")
    }
  }
}

struct FilesError: LocalizedError {
  let message: String
  init(_ message: String) { self.message = message }
  var errorDescription: String? { message }
}
