package skills

import (
	"fmt"
	"os"
	"path/filepath"
)

// BundleFile describes one bundled file in a skill's manifest as returned by
// the skills resolve endpoints: a relative forward-slash Path, a presigned
// DownloadURL, and the SizeBytes the server reported for it.
type BundleFile struct {
	Path        string
	DownloadURL string
	SizeBytes   int64
}

// Bundle is the resolved content of a skill ready to be written to disk: the
// literal SKILL.md bytes plus its bundled files.
type Bundle struct {
	RawSkillMD string
	Files      []BundleFile
}

// Downloader fetches the contents of a presigned URL into dest, creating
// parent directories as needed. Callers inject their own implementation so the
// skills package stays free of cobra/HTTP-context concerns.
type Downloader func(url, dest string) error

// WrittenFile records one file written to disk during a reconstruction.
type WrittenFile struct {
	// Dest is the absolute (or dir-relative) on-disk path written.
	Dest string
	// SizeBytes is the reported size of the file (0 for SKILL.md, where the
	// literal byte length is used instead).
	SizeBytes int64
}

// PlanReconstruct validates every manifest path against dir using SafeJoin and
// returns the resolved destination for SKILL.md plus each bundled file. It
// writes nothing — callers use it for dry-run previews and to fail fast on a
// malicious path before any directory is created.
//
// The returned slice is aligned with b.Files; the SKILL.md destination is
// returned separately as it is not part of the bundle manifest.
func PlanReconstruct(dir string, b Bundle) (skillMDDest string, fileDests []string, err error) {
	fileDests = make([]string, len(b.Files))
	for i, f := range b.Files {
		dest, err := SafeJoin(dir, f.Path)
		if err != nil {
			return "", nil, err
		}
		fileDests[i] = dest
	}
	return filepath.Join(dir, "SKILL.md"), fileDests, nil
}

// Reconstruct writes a skill bundle into dir: it creates dir, writes SKILL.md
// verbatim, and downloads every bundled file via download. Every manifest path
// is validated with SafeJoin before any write happens, so a malicious path
// aborts the whole operation instead of leaving a partial folder behind.
//
// It returns the list of files written (SKILL.md first, then bundled files in
// manifest order) so callers can print a per-file summary.
func Reconstruct(dir string, b Bundle, download Downloader) ([]WrittenFile, error) {
	skillMDDest, fileDests, err := PlanReconstruct(dir, b)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	written := make([]WrittenFile, 0, len(b.Files)+1)

	// SKILL.md — raw bytes, verbatim.
	if err := os.WriteFile(skillMDDest, []byte(b.RawSkillMD), 0o644); err != nil {
		return nil, err
	}
	written = append(written, WrittenFile{Dest: skillMDDest, SizeBytes: int64(len(b.RawSkillMD))})

	for i, f := range b.Files {
		if f.DownloadURL == "" {
			return written, fmt.Errorf("no download URL for %s", f.Path)
		}
		if err := download(f.DownloadURL, fileDests[i]); err != nil {
			return written, fmt.Errorf("download %s: %w", f.Path, err)
		}
		written = append(written, WrittenFile{Dest: fileDests[i], SizeBytes: f.SizeBytes})
	}

	return written, nil
}
