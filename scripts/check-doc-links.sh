#!/usr/bin/env bash
# Reports relative markdown links whose target does not exist. Exits 1 if any are broken.
set -u
cd "$(dirname "$0")/.." || exit 2

status=0
while IFS= read -r file; do
  dir=$(dirname "$file")
  while IFS= read -r target; do
    target=${target%% \"*}
    target=${target%%#*}
    [ -z "$target" ] && continue
    case "$target" in http://*|https://*|mailto:*) continue ;; esac
    if [ ! -e "$dir/$target" ]; then
      echo "$file: broken -> $target"
      status=1
    fi
  done < <(grep -o '](<\{0,1\}[^)]*)' "$file" | sed 's/^](<\{0,1\}//; s/>\{0,1\})$//')
done < <(find . -name '*.md' -not -path './.git/*' -not -path './.claude/*' | sort)

exit $status
