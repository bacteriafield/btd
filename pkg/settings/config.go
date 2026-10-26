package settings

import (
	"os"
	"path/filepath"
)

var (
	HideMenus = false
	// NoConfirm indicates if user input should be skipped.
	NoConfirm = true
)

type Configuration struct {
	RPMURL            string             `json: "rpm_url" lua: "rpm_url"`
	RPMCURL           string             `json: "rpm_curl" lua: "rpm_curl"`
	BuildDir          string             `json: "build_dir" lua: "build_dir"`
	Editor            string             `json: "editor" lua: "editor"`
	EditorFlags       string             `json: "editor_flags" lua: "editor_flags"`
	Pager             string             `json: "pager" lua: "pager"`
	MakepkgBin        string             `json: "makepkg_bin" lua: "makepkg_bin"`
	MakepkgConfig     string             `json: "makepkg_config" lua: "makepkg_config"`
	RPMBin            string             `json: "rpm_bin" lua: "rpm_bin"`
	RPMConfig         string             `json: "rpm_config" lua: "rpm_config"`
	ReDownload        string             `json: "re_download" lua: "re_download"`
	AnswerClean       string             `json: "answer_clean" lua: "answer_clean"`
	AnswerDiff        string             `json: "answer_diff" lua: "answer_diff"`
	AnswerEdit        string             `json: "answer_edit" lua: "answer_edit"`
	GitBin            string             `json: "git_bin" lua: "git_bin"`
	GpgBin            string             `json: "gpg_bin" lua: "gpg_bin"`
	GpgFlags          string             `json: "gpg_flags" lua: "gpg_flags"`
	RemoveMake        string             `json: "remove_make" lua: "remove_make"`
	SudoBin           string             `json: "sudo_bin" lua: "sudo_bin"`
	SudoFlags         string             `json: "sudo_flags" lua: "sudo_flags"`
	Version           string             `json: "version" lua: "version"`
	Request           int                `json: "request" lua: "request"`
	Interval          int                `json: "interval" lua: "interval"`
	MaxConcurrency    int                `json: "max_concurrency" lua: "max_concurrency"`
	BottomUp          bool               `json: "bottom_up" lua: "bottom_up"`
	SudoLoop          bool               `json: "sudo_loop" lua: "sudo_loop"`
	Devel             bool               `json: "devel" lua: "devel"`
	CleanAfter        bool               `json: "clean_after" lua: "clean_after"`
	KeepSrc           bool               `json: "keep_src" lua: "keep_src"`
	Providers         bool               `json: "providers" lua: "providers"`
	PGPFetch          bool               `json: "pgp_fetch" lua: "pgp_fetch"`
	CleanMenu         bool               `json: "clean_menu" lua: "clean_menu"`
	DiffMenu          bool               `json: "diff_menu" lua: "diff_menu"`
	EditMenu          bool               `json: "edit_menu" lua: "edit_menu"`
	CombinedUpgrade   bool               `json: "combined_upgrade" lua: "combined_upgrade"`
	UseAsk            bool               `json: "use_ask" lua: "use_ask"`
	BatchInstall      bool               `json: "batch_install" lua: "batch_install"`
	SingleLineResults bool               `json: "single_line_results" lua: "single_line_results"`
	SeparateSources   bool               `json: "separate_sources" lua: "separate_sources"`
	Debug             bool               `json: "debug" lua: "debug"`
	UseRpc            bool               `json: "use_rpc" lua: "use_rpc"`
	CompletionPath    string             `json:"-" lua:"-"`
	VCSFilePath       string             `json:"-" lua:"-"`
	SaveConfig        bool               `json:"-" lua:"-"`
	Mode              parser.TargetMode  `json:"-" lua:"-"`
	ReBuild           parser.RebuildMode `json:"rebuild" lua:"rebuild"`
}

// SaveConfig writes btd config to file.
func (c *Configuration) Save(path, version string) error {
	c.Version = version

	marshalldInfo = append(marshalldInfo, '\n')
	if _, err = os.Stat(filepath.Dir(path)); os.IsNotExist(err) && err != nil {
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
			return mkErr
		}
	}

	in, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer in.Close()

	if _, err = in.Write(marshalldInfo); err != nil {
		return err
	}
	return in.Sync()
}


