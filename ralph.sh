#!/bin/bash
# Ralph Wiggum - Industrial Edge Middleware AI Agent
# Usage: ./ralph.sh [max_iterations]

set -e

MAX_ITERATIONS=${1:-10}
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PRD_FILE="$SCRIPT_DIR/.claude/skills/ralph/prd.json"
PROGRESS_FILE="$SCRIPT_DIR/.claude/skills/ralph/progress.txt"
ARCHIVE_DIR="$SCRIPT_DIR/.claude/skills/ralph/archive"
LAST_BRANCH_FILE="$SCRIPT_DIR/.claude/skills/ralph/.last-branch"

# Archive previous run if branch changed
if [ -f "$PRD_FILE" ] && [ -f "$LAST_BRANCH_FILE" ]; then
  CURRENT_BRANCH=$(python3 -c "import json; print(json.load(open('$PRD_FILE')).get('branchName', ''))" 2>/dev/null || echo "")
  LAST_BRANCH=$(cat "$LAST_BRANCH_FILE" 2>/dev/null || echo "")

  if [ -n "$CURRENT_BRANCH" ] && [ -n "$LAST_BRANCH" ] && [ "$CURRENT_BRANCH" != "$LAST_BRANCH" ]; then
    # Archive the previous run
    DATE=$(date +%Y-%m-%d)
    FOLDER_NAME=$(echo "$LAST_BRANCH" | sed 's|^ralph/||')
    ARCHIVE_FOLDER="$ARCHIVE_DIR/$DATE-$FOLDER_NAME"

    echo "Archiving previous run: $LAST_BRANCH"
    mkdir -p "$ARCHIVE_FOLDER"
    [ -f "$PRD_FILE" ] && cp "$PRD_FILE" "$ARCHIVE_FOLDER/"
    [ -f "$PROGRESS_FILE" ] && cp "$PROGRESS_FILE" "$ARCHIVE_FOLDER/"
    echo "   Archived to: $ARCHIVE_FOLDER"

    # Reset progress file for new run
    echo "# Ralph Progress - Industrial Edge Middleware" > "$PROGRESS_FILE"
    echo "Branch: $CURRENT_BRANCH" >> "$PROGRESS_FILE"
    echo "Started: $(date)" >> "$PROGRESS_FILE"
    echo "---" >> "$PROGRESS_FILE"
  fi
fi

# Track current branch
if [ -f "$PRD_FILE" ]; then
  CURRENT_BRANCH=$(python3 -c "import json; print(json.load(open('$PRD_FILE')).get('branchName', ''))" 2>/dev/null || echo "")
  if [ -n "$CURRENT_BRANCH" ]; then
    echo "$CURRENT_BRANCH" > "$LAST_BRANCH_FILE"
  fi
fi

# Initialize progress file if it doesn't exist
if [ ! -f "$PROGRESS_FILE" ]; then
  echo "# Ralph Progress - Industrial Edge Middleware" > "$PROGRESS_FILE"
  echo "Started: $(date)" >> "$PROGRESS_FILE"
  echo "---" >> "$PROGRESS_FILE"
fi

echo "Starting Ralph - Industrial Edge Middleware Agent"
echo "Max iterations: $MAX_ITERATIONS"
echo "PRD: $PRD_FILE"
echo ""

# Change to script directory so file references work
cd "$SCRIPT_DIR"

for i in $(seq 1 $MAX_ITERATIONS); do
  echo ""
  echo "==============================================================="
  echo "  Ralph Iteration $i of $MAX_ITERATIONS"
  echo "==============================================================="

  # Build the prompt
  PROMPT="@$PRD_FILE @$PROGRESS_FILE
You are Ralph, an AI developer agent working on the Industrial Edge Middleware project.

Your task is to implement the highest-priority incomplete feature from the PRD.

INSTRUCTIONS:
1. Read the PRD (prd.json) and find the user story with highest priority where passes is false
2. Implement ONLY that one feature
3. Check that the code compiles: go build ./...
4. Update the PRD: mark the story as passes: true and add notes about what was done
5. Append progress to progress.txt with date and description
6. Make a git commit with message describing the feature

IMPORTANT:
- Work on ONLY ONE feature per iteration
- Do NOT ask for permission to edit files - just edit them directly
- After completing a feature, explicitly state which US you completed
- If all features are complete, output: <promise>COMPLETE</promise>

Start by reading the PRD to see what needs to be done."

  # Run Claude with proper flags for autonomous operation
  echo "Running Claude Code..."
  OUTPUT=$(claude --permission-mode bypassPermissions --print -p "$PROMPT" 2>&1) || true

  # Show output
  echo "$OUTPUT"

  # Check for completion signal
  if echo "$OUTPUT" | grep -q "<promise>COMPLETE</promise>"; then
    echo ""
    echo "==============================================================="
    echo "  Ralph completed all tasks!"
    echo "==============================================================="
    echo "Completed at iteration $i of $MAX_ITERATIONS"
    exit 0
  fi

  # Show remaining tasks
  if [ -f "$PRD_FILE" ]; then
    REMAINING=$(python3 -c "import json; prd=json.load(open('$PRD_FILE')); print(sum(1 for s in prd['userStories'] if not s.get('passes', False)))" 2>/dev/null || echo "?")
    echo ""
    echo "Remaining tasks: $REMAINING"
  fi

  echo "Iteration $i complete. Continuing..."
  sleep 1
done

echo ""
echo "==============================================================="
echo "  Ralph reached max iterations ($MAX_ITERATIONS)"
echo "==============================================================="
echo "Check $PROGRESS_FILE for status."
exit 1
