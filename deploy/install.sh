#!/bin/sh
# Install the hotwatch agent on this Linux host.
#
#   sh install.sh ssh|tailscale [path/to/hotwatch-agent] [path/to/hotwatch-agent.service]
#
# Every step is printed and needs a "y" answer (read from the terminal).
# Nothing but the agent itself is installed: no packages, no downloads.
set -eu

usage() {
	echo "usage: $0 ssh|tailscale [agent-binary] [unit-file]" >&2
	exit 64
}

[ $# -ge 1 ] && [ $# -le 3 ] || usage
mode=$1
case $mode in ssh | tailscale) ;; *) usage ;; esac

here=$(cd "$(dirname "$0")" && pwd)
bin=${2:-$here/hotwatch-agent}
unit=${3:-$here/hotwatch-agent.service}

if [ "$(id -u)" -ne 0 ]; then
	echo "install.sh: must run as root" >&2
	exit 1
fi
[ -f "$bin" ] || {
	echo "install.sh: agent binary not found: $bin" >&2
	exit 1
}
[ -f "$unit" ] || {
	echo "install.sh: unit file not found: $unit" >&2
	exit 1
}
[ -r /dev/tty ] || {
	echo "install.sh: needs a terminal to ask for confirmation" >&2
	exit 1
}
if [ "$mode" = tailscale ] && ! command -v tailscale >/dev/null 2>&1; then
	echo "install.sh: tailscale mode needs an installed, running tailscale (not installed by this script)" >&2
	exit 1
fi

# confirm "description": print the step and require y/Y.
confirm() {
	printf '\n==> %s\n    Proceed? [y/N] ' "$1"
	answer=
	read -r answer </dev/tty || answer=
	case $answer in y | Y) ;; *)
		echo "Aborted; nothing further was changed." >&2
		exit 1
		;;
	esac
}

if id hotwatch >/dev/null 2>&1; then
	echo "System user hotwatch already exists."
else
	nologin=$(command -v nologin || echo /usr/sbin/nologin)
	confirm "Create system user hotwatch (no login shell, no home directory)"
	useradd --system --no-create-home --shell "$nologin" hotwatch
fi

if getent group docker >/dev/null 2>&1; then
	confirm "Add hotwatch to group docker (needed for container actions; this is root-equivalent access to Docker)"
	usermod -aG docker hotwatch
else
	echo "Group docker not found: container features will be unavailable."
fi

confirm "Install $bin to /usr/local/bin/hotwatch-agent"
install -m 0755 -o root -g root "$bin" /usr/local/bin/hotwatch-agent

confirm "Create /etc/hotwatch (0750 root:hotwatch) and /var/log/hotwatch (0750 hotwatch:hotwatch)"
install -d -m 0750 -o root -g hotwatch /etc/hotwatch
install -d -m 0750 -o hotwatch -g hotwatch /var/log/hotwatch

if [ -e /etc/hotwatch/agent.json ]; then
	echo "/etc/hotwatch/agent.json already exists; keeping it (rotate with: hotwatch-agent init --mode $mode --rotate)."
else
	confirm "Write /etc/hotwatch/agent.json for mode $mode and print the access token (shown once, stored only as a hash)"
	/usr/local/bin/hotwatch-agent init --mode "$mode"
fi
chown hotwatch:hotwatch /etc/hotwatch/agent.json
chmod 0600 /etc/hotwatch/agent.json

confirm "Install the systemd unit /etc/systemd/system/hotwatch-agent.service"
install -m 0644 -o root -g root "$unit" /etc/systemd/system/hotwatch-agent.service

confirm "Run: systemctl daemon-reload && systemctl enable --now hotwatch-agent"
systemctl daemon-reload
systemctl enable --now hotwatch-agent

echo
echo "Done. Check with: systemctl status hotwatch-agent"
