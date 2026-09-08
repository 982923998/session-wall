import AppKit
import ApplicationServices

func emit(_ value: Any) {
    if let data = try? JSONSerialization.data(withJSONObject: value, options: [.sortedKeys]),
       let text = String(data: data, encoding: .utf8) { print(text) }
}

func attribute(_ element: AXUIElement, _ name: CFString) -> AnyObject? {
    var value: CFTypeRef?
    return AXUIElementCopyAttributeValue(element, name, &value) == .success ? value : nil
}

// This controller uses the user-authorized macOS Accessibility API only.
// It never connects to Codex's private control pipe or changes its launch chain.
let arguments = Array(CommandLine.arguments.dropFirst())
let trusted = AXIsProcessTrusted()
if arguments == ["permission-status"] {
    emit(["accessibility": trusted, "minimum_macos": "13.0"] as [String: Any])
    exit(0)
}
var request: [String: Any] = [:]
if arguments.count == 2 && arguments[0] == "request" {
    guard let data = try? Data(contentsOf: URL(fileURLWithPath: arguments[1])), data.count < 128 * 1024,
          let parsed = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else {
        emit(["error": "invalid_request"]); exit(2)
    }
    request = parsed
} else if arguments != ["inspect"] { emit(["error": "Expected permission-status, inspect or request file"]); exit(2) }
guard trusted else {
    emit(["error": "accessibility_permission_required"])
    exit(3)
}
guard let application = NSRunningApplication.runningApplications(withBundleIdentifier: "com.openai.codex").first else {
    emit(["error": "codex_not_running"])
    exit(4)
}
let root = AXUIElementCreateApplication(application.processIdentifier)
AXUIElementSetMessagingTimeout(root, 2)
AXUIElementSetAttributeValue(root, "AXManualAccessibility" as CFString, kCFBooleanTrue)
RunLoop.current.run(until: Date().addingTimeInterval(0.5))
struct ActionFailure: Error { let message: String }
func check(_ error: AXError) throws {
    if error != .success { throw ActionFailure(message: "accessibility_error_\(error.rawValue)") }
}
func target(_ action: [String: Any]) throws -> AXUIElement {
    guard let path = action["path"] as? [Int], !path.isEmpty,
          let role = action["role"] as? String, !role.isEmpty else { throw ActionFailure(message: "target_identity_required") }
    var node = root
    for index in path {
        let children = attribute(node, kAXChildrenAttribute as CFString) as? [AXUIElement] ?? []
        guard children.indices.contains(index) else { throw ActionFailure(message: "stale_target") }
        node = children[index]
    }
    guard attribute(node, kAXRoleAttribute as CFString) as? String == role else { throw ActionFailure(message: "target_role_changed") }
    for (key, name) in [("title", kAXTitleAttribute), ("description", kAXDescriptionAttribute), ("identifier", "AXIdentifier")] {
        if let expected = action[key] as? String, (attribute(node, name as CFString) as? String ?? "") != expected {
            throw ActionFailure(message: "target_identity_changed")
        }
    }
    return node
}
func performActions() throws -> [[String: Any]] {
    let actions = request["actions"] as? [[String: Any]] ?? []
    guard actions.count <= 20 else { throw ActionFailure(message: "too_many_actions") }
    let pasteboard = NSPasteboard.general
    let preserve = request["preserveClipboard"] as? Bool ?? false
    let saved = preserve ? (pasteboard.pasteboardItems ?? []).map { item in
        Dictionary(uniqueKeysWithValues: item.types.compactMap { type in item.data(forType: type).map { (type, $0) } })
    } : []
    defer {
        if preserve {
            pasteboard.clearContents()
            pasteboard.writeObjects(saved.map { values -> NSPasteboardItem in
                let item = NSPasteboardItem()
                for (type, data) in values { item.setData(data, forType: type) }
                return item
            })
        }
    }
    var results: [[String: Any]] = []
    for action in actions {
        switch action["op"] as? String ?? "" {
        case "activate": application.activate(options: [.activateIgnoringOtherApps])
        case "openThread":
            guard let id = action["threadId"] as? String, UUID(uuidString: id) != nil,
                  let url = URL(string: "codex://threads/\(id)") else { throw ActionFailure(message: "invalid_thread_id") }
            guard NSWorkspace.shared.open(url) else { throw ActionFailure(message: "navigation_failed") }
        case "press": try check(AXUIElementPerformAction(try target(action), kAXPressAction as CFString))
        case "focus": try check(AXUIElementSetAttributeValue(try target(action), kAXFocusedAttribute as CFString, kCFBooleanTrue))
        case "setValue":
            guard let value = action["value"] as? String, value.utf8.count <= 64 * 1024 else { throw ActionFailure(message: "invalid_value") }
            try check(AXUIElementSetAttributeValue(try target(action), kAXValueAttribute as CFString, value as CFString))
        case "key":
            guard let key = action["key"] as? Int, (0...127).contains(key),
                  NSWorkspace.shared.frontmostApplication?.processIdentifier == application.processIdentifier else {
                throw ActionFailure(message: "codex_must_be_frontmost")
            }
            var flags: CGEventFlags = []
            for modifier in action["modifiers"] as? [String] ?? [] {
                switch modifier {
                case "command": flags.insert(.maskCommand)
                case "shift": flags.insert(.maskShift)
                case "option": flags.insert(.maskAlternate)
                case "control": flags.insert(.maskControl)
                default: throw ActionFailure(message: "invalid_modifier")
                }
            }
            for down in [true, false] {
                guard let event = CGEvent(keyboardEventSource: nil, virtualKey: CGKeyCode(key), keyDown: down) else { throw ActionFailure(message: "keyboard_event_failed") }
                event.flags = flags; event.postToPid(application.processIdentifier)
            }
        case "readClipboard": results.append(["text": pasteboard.string(forType: .string) ?? ""])
        case "writeClipboard":
            guard preserve, let text = action["text"] as? String else { throw ActionFailure(message: "clipboard_preservation_required") }
            pasteboard.clearContents(); pasteboard.setString(text, forType: .string)
        case "wait": RunLoop.current.run(until: Date().addingTimeInterval(min(max(action["seconds"] as? Double ?? 0.2, 0), 2)))
        default: throw ActionFailure(message: "unknown_action")
        }
        RunLoop.current.run(until: Date().addingTimeInterval(0.2))
    }
    return results
}
let results: [[String: Any]]
do { results = try performActions() }
catch let error as ActionFailure { emit(["error": error.message]); exit(5) }
catch { emit(["error": error.localizedDescription]); exit(5) }
var nodes: [[String: Any]] = []
func walk(_ element: AXUIElement, depth: Int, path: [Int]) {
    guard depth < 50 && nodes.count < 2000 else { return }
    let role = attribute(element, kAXRoleAttribute as CFString) as? String ?? ""
    let title = attribute(element, kAXTitleAttribute as CFString) as? String ?? ""
    let description = attribute(element, kAXDescriptionAttribute as CFString) as? String ?? ""
    let value = attribute(element, kAXValueAttribute as CFString) as? String ?? ""
    if role == "AXMenuBar" && request["includeMenus"] as? Bool != true { return }
    let identifier = attribute(element, "AXIdentifier" as CFString) as? String ?? ""
    nodes.append(["index": nodes.count, "depth": depth, "path": path, "role": role,
                  "title": String(title.prefix(240)), "description": String(description.prefix(240)),
                  "value": String(value.prefix(2000)), "identifier": identifier,
                  "enabled": attribute(element, kAXEnabledAttribute as CFString) as? Bool ?? false])
    for (index, child) in (attribute(element, kAXChildrenAttribute as CFString) as? [AXUIElement] ?? []).enumerated() {
        walk(child, depth: depth + 1, path: path + [index])
    }
}
walk(root, depth: 0, path: [])
emit(["pid": application.processIdentifier, "nodes": nodes, "results": results] as [String: Any])
