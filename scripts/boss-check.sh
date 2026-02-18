#!/bin/bash
# broadcast.sh: Send a command to all agent sessions

COMMAND="boss check"

# Get a list of all active tmux sessions
SESSIONS=$(tmux list-sessions -F "#S")

for SESSION in $SESSIONS; do
    # Only target sessions created by your agent deck
    if [[ $SESSION == *"agent"* ]]; then
        echo "Broadcasting to $SESSION..."
        # C-m at the end simulates pressing "Enter"
        tmux send-keys -t "$SESSION" "$COMMAND"
	sleep .1
	tmux send-keys -t "$SESSION" C-m

    fi
done
