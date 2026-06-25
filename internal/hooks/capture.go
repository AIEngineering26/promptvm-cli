package hooks

// CaptureHookSlug is the tracker/_slug identifier for the context-sync capture
// hook. It scopes MergeHook/RemoveHook so re-running `sync init` is idempotent
// and never collides with marketplace-installed hooks.
const CaptureHookSlug = "promptvm-context-sync"

// CaptureHookCommand is the command every capture hook event invokes. The
// command is identical across events and platforms; `sync run` reads the event
// name from stdin and self-detaches, so non-blocking behavior does not depend
// on Claude Code's `async` field or a `setsid` binary (HOOK-3 / DX-6).
const CaptureHookCommand = "promptvm sync run"

// BuildCaptureFragment builds a Claude Code settings.json "hooks" fragment for
// the given events. Each event gets a single command-hook matcher group tagged
// with CaptureHookSlug so it is trackable and removable. The fragment is shaped
// for the settings "hooks" key (HOOK-7), not PromptVM's internal "events" key.
//
// SessionStart is reconcile-only and stdout-silent (HOOK-4); it carries no
// matcher so it fires on all sources (startup|resume|clear|compact).
func BuildCaptureFragment(events []string) map[string]interface{} {
	fragment := make(map[string]interface{}, len(events))
	for _, ev := range events {
		handler := map[string]interface{}{
			"type":    "command",
			"command": CaptureHookCommand,
			// Belt-and-suspenders: request async from Claude Code versions
			// that honor it. `sync run` self-detaches regardless (HOOK-3).
			"async":   true,
			"timeout": 10,
		}
		group := map[string]interface{}{
			"matcher": "",
			"_slug":   CaptureHookSlug,
			"hooks":   []interface{}{handler},
		}
		fragment[ev] = []interface{}{group}
	}
	return fragment
}

// CaptureEventsInstalled returns the capture-hook event names currently present
// in the settings, identified by the CaptureHookSlug tag.
func (s *Settings) CaptureEventsInstalled() []string {
	hooks := s.Hooks()
	var out []string
	for eventName, matchers := range hooks {
		list, ok := toSlice(matchers)
		if !ok {
			continue
		}
		for _, m := range list {
			mMap, ok := m.(map[string]interface{})
			if !ok {
				continue
			}
			if slug, _ := mMap["_slug"].(string); slug == CaptureHookSlug {
				out = append(out, eventName)
				break
			}
		}
	}
	return out
}
