package shellutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	startMarker = "# >>> luggage initialize >>>"
	endMarker   = "# <<< luggage initialize <<<"
)

func ZshSnippet() string {
	return `# >>> luggage initialize >>>
if command -v luggage >/dev/null 2>&1; then
  if [[ -z "${__luggage_hooked:-}" ]]; then
    __luggage_hooked=1
    zmodload zsh/datetime 2>/dev/null || true

    _luggage_resolve_command() {
      emulate -L zsh
      setopt localoptions noshwordsplit
      local raw="$1"
      local -a parts
      parts=(${(z)raw})
      local cmd="${parts[1]}"
      if [[ -z "$cmd" ]]; then
        print -r -- ""
        return
      fi
      if [[ -n "${aliases[$cmd]-}" ]]; then
        print -r -- "alias:${cmd}=${aliases[$cmd]}"
        return
      fi
      if (( $+functions[$cmd] )); then
        print -r -- "function:${cmd}"
        return
      fi
      local info
      info=$(whence -w -- "$cmd" 2>/dev/null)
      if [[ "$info" == *": builtin" ]]; then
        print -r -- "builtin:${cmd}"
        return
      fi
      local path
      path=$(whence -p -- "$cmd" 2>/dev/null)
      if [[ -n "$path" ]]; then
        print -r -- "exec:${path}"
      else
        print -r -- "unknown:${cmd}"
      fi
    }

    _luggage_preexec() {
      __luggage_cmd="$1"
      __luggage_started_at="$EPOCHREALTIME"
    }

    _luggage_precmd() {
      local exit_code=$?
      if [[ -z "${__luggage_cmd:-}" || -z "${__luggage_started_at:-}" ]]; then
        return
      fi

      local typed="$__luggage_cmd"
      local started_at="$__luggage_started_at"
      local first_token
      local -a parts
      parts=(${(z)typed})
      first_token="${parts[1]}"

      unset __luggage_cmd __luggage_started_at

      if [[ -z "$first_token" || "$first_token" == "luggage" ]]; then
        return
      fi

      local ended_at="$EPOCHREALTIME"
      local resolved
      resolved="$(_luggage_resolve_command "$typed")"

      command luggage record \
        --started-at "$started_at" \
        --ended-at "$ended_at" \
        --typed "$typed" \
        --resolved "$resolved" \
        --exit-code "$exit_code" \
        --cwd "$PWD" \
        >/dev/null 2>&1 &!
    }

    autoload -Uz add-zsh-hook
    add-zsh-hook preexec _luggage_preexec
    add-zsh-hook precmd _luggage_precmd
  fi

  if [[ -d "$HOME/.zfunc" && ${fpath[(Ie)$HOME/.zfunc]} -eq 0 ]]; then
    fpath=("$HOME/.zfunc" $fpath)
  fi
  if [[ -f "$HOME/.zfunc/_luggage" ]]; then
    autoload -Uz _luggage
    compdef _luggage luggage
  fi
fi
# <<< luggage initialize <<<
`
}

func ZshCompletion() string {
	return `#compdef luggage

_luggage_recent_time_commands() {
  local prefix="$1"
  local -a cmds
  cmds=("${(@f)$(command luggage __complete time "$prefix" 2>/dev/null)}")
  if (( ${#cmds[@]} > 0 )); then
    compadd -Q -U -S '' -- "${cmds[@]}"
  fi
}

_luggage() {
  local cur prev subcmd cmd_idx arg_idx
  local -a top_cmds config_keys

  top_cmds=(
    install
    init
    time
    sessions
    config
    completion
    version
    help
  )
  config_keys=(
    default_days
    default_granularity
    default_inclusion
    default_view
    session_cutoff_ms
    anomaly_window
    anomaly_sigma
  )

  cur="${words[CURRENT]}"
  prev="${words[CURRENT-1]}"

  cmd_idx=1
  if [[ "${words[1]}" == "luggage" ]]; then
    cmd_idx=2
  fi

  if (( CURRENT <= cmd_idx )); then
    compadd -Q -U -- "${top_cmds[@]}" --help -h --version -v
    return
  fi

  subcmd="${words[cmd_idx]}"
  arg_idx=$((cmd_idx + 1))

  case "$subcmd" in
    install)
      if [[ "$prev" == "--bin-dir" ]]; then
        _files -/
        return
      fi
      if [[ "$prev" == "--shell" ]]; then
        compadd -Q -U -- zsh
        return
      fi
      compadd -Q -U -- --bin-dir --shell --no-shell
      ;;
    init)
      if (( CURRENT == arg_idx )); then
        compadd -Q -U -- zsh
        return
      fi
      compadd -Q -U -- --install
      ;;
    completion)
      if (( CURRENT == arg_idx )); then
        compadd -Q -U -- zsh
      fi
      ;;
    config)
      if (( CURRENT == arg_idx )); then
        compadd -Q -U -- get set
        return
      fi
      if [[ "${words[arg_idx]}" == "get" || "${words[arg_idx]}" == "set" ]]; then
        compadd -Q -U -- "${config_keys[@]}"
      fi
      ;;
    sessions)
      if [[ "$prev" == "--view" ]]; then
        compadd -Q -U -- typed resolved
        return
      fi
      compadd -Q -U -- --days --view
      ;;
    time)
      if [[ "$prev" == "--pwd" || "$prev" == "--cwd" || "$prev" == "--git-root" ]]; then
        _files -/
        return
      fi
      if [[ "$prev" == "--granularity" ]]; then
        compadd -Q -U -- hourly daily weekly
        return
      fi
      if [[ "$prev" == "--view" ]]; then
        compadd -Q -U -- typed resolved
        return
      fi
      if [[ "$prev" == "--window" ]]; then
        compadd -Q -U -- active full
        return
      fi
      if [[ "$cur" == --* ]]; then
        compadd -Q -U -- --days --granularity --view --window --show-empty --pwd --cwd --git-root --here --this-repo --include-sessions --success-only
        return
      fi
      _luggage_recent_time_commands "$cur"
      ;;
    version|help)
      ;;
    *)
      compadd -Q -U -- "${top_cmds[@]}"
      ;;
  esac
}

compdef _luggage luggage
`
}

func InstallZshSnippet() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	zshrc := filepath.Join(home, ".zshrc")
	content, err := os.ReadFile(zshrc)
	if err != nil {
		return "", err
	}
	text := string(content)
	snippet := ZshSnippet()

	if strings.Contains(text, startMarker) && strings.Contains(text, endMarker) {
		start := strings.Index(text, startMarker)
		end := strings.Index(text, endMarker)
		if end < start {
			return "", fmt.Errorf("invalid existing luggage markers in .zshrc")
		}
		end += len(endMarker)
		updated := text[:start] + snippet + text[end:]
		if !strings.HasSuffix(updated, "\n") {
			updated += "\n"
		}
		if err := os.WriteFile(zshrc, []byte(updated), 0o644); err != nil {
			return "", err
		}
		return zshrc, nil
	}

	updated := strings.TrimRight(text, "\n") + "\n\n" + snippet
	if err := os.WriteFile(zshrc, []byte(updated), 0o644); err != nil {
		return "", err
	}
	return zshrc, nil
}

func InstallZshCompletionFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".zfunc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "_luggage")
	content := ZshCompletion()
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
