#!/bin/zsh
# Used by the uninstaller to remove the agent and all related files and services on macOS

# Path and Labels
OLD_LABEL="com.it-novum.openitcockpit.agent"
NEW_LABEL="io.openitcockpit.agent"
AGENT_DIR="/Applications/openitcockpit-agent"
LOG_DIR="/Library/Logs/openitcockpit-agent"

# Function to cleanly remove a service
remove_service() {
    local label=$1
    local plist="/Library/LaunchDaemons/${label}.plist"

    if /bin/launchctl list | grep -q "$label"; then
        # Modern macOS 15 bootout (stops and unloads)
        /bin/launchctl bootout system "$plist" 2>/dev/null
        # Legacy Fallback
        /bin/launchctl unload -F "$plist" 2>/dev/null
    fi

    # Delete plist at system location
    rm -f "$plist"
}

# 1. Remove both possible services (ensure migration)
remove_service "$OLD_LABEL"
remove_service "$NEW_LABEL"

# 2. Delete files in the app directory
if [ -d "$AGENT_DIR" ]; then
    rm -rf "$AGENT_DIR"
fi

# 3. Remove logs
if [ -d "$LOG_DIR" ]; then
    rm -rf "$LOG_DIR"
fi

# 4. Remove any remnants in /etc (if present)
if [ -d "/private/etc/openitcockpit-agent" ]; then
    rm -rf "/private/etc/openitcockpit-agent"
fi

exit 0
