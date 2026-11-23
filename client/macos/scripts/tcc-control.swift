// The privacy control of the image input run (macos-image-input.yml): an app of its own (opened with `open`, its own
// bundle id) that reads a file in ~/Desktop, ~/Documents or ~/Downloads by itself, with no drop or paste behind it.
// On a Mac that guards those folders this read asks the person first ("… would like to access files in your …
// folder"); the run compares Mewla Dev's drops and pastes with it.
//   tcc-control <folder> <out file>   writes "read <n> bytes" or "refused: <error>" to <out file>, then quits
import Foundation

let args = CommandLine.arguments
let file = URL(fileURLWithPath: NSHomeDirectory()).appendingPathComponent(args[1]).appendingPathComponent("control.txt")
let said: String
do {
  said = "read \(try Data(contentsOf: file).count) bytes"
} catch {
  said = "refused: \(error.localizedDescription)"
}
try? said.write(toFile: args[2], atomically: true, encoding: .utf8)
