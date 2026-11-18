package ops

import "github.com/banma05/git-glance/core/internal/settings"

// gitFailed and verifyFailed are op-log results of a git command that ran:
// git itself failed, or it succeeded but the check afterwards did not.
func gitFailed(err error) string      { return "git-failed: " + failDetail(err) }
func verifyFailed(what string) string { return "verify-failed: " + what }

// logRan writes the op-log entry of a mutating git command once it has run,
// whatever the outcome: result is "ok", gitFailed(...) or verifyFailed(...),
// and a non-empty detail is added to the entry's Detail. Every operation goes
// through here, so nothing that changed the repository is left out of the
// log. It returns the text of a failure to write the log, or "".
func logRan(st settings.Store, e settings.OpEntry, result, detail string) string {
	e.Result = result
	e.Detail = joinDetail(e.Detail, detail)
	if err := st.AppendOp(e); err != nil {
		return err.Error()
	}
	return ""
}
