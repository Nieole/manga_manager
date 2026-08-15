#!/usr/bin/env bash
# 文档与注释风格检查器。规范见 docs/agents/doc-style.md。
#
# 拦截（退出码 1）：注释头超预算；行号与现状统计数字。
# 提示（不影响退出码）：疑似历史叙述的词，误报多，需人工按三段判定处置。
#
# 包注释的存在性由 .golangci.yml 的 ST1000 强制，这里不重复检查。

set -uo pipefail

cd "$(dirname "$0")/.." || exit 1

FAIL=0

# 生成物不受本规范约束：Go 侧由 sqlc 产出，TS 侧由 cmd/tsgen 产出。
is_generated() {
  case "$1" in
    web/src/api/generated.ts) return 0 ;;
  esac
  head -5 "$1" 2>/dev/null | grep -q '^// Code generated'
}

# ---------------------------------------------------------------- 预算

# 数注释头的内容行数：跳过 build 指令，忽略纯分隔行（// 、 * 、/** 、*/）。
# 注释块后紧跟 package 声明的是包注释（预算 8 行），中间隔了空行的是文件头（预算 5 行）。
check_budget_go() {
  awk -v file="$1" '
    { lines[NR] = $0 }
    END {
      i = 1
      while (i <= NR && (lines[i] ~ /^\/\/go:build/ || lines[i] ~ /^\/\/ \+build/ || lines[i] == "")) i++
      count = 0
      start = i
      while (i <= NR && lines[i] ~ /^\/\//) {
        if (lines[i] !~ /^\/\/[[:space:]]*$/) count++
        i++
      }
      if (i == start) exit
      if (lines[i] ~ /^package /) { budget = 8; kind = "包注释" } else { budget = 5; kind = "文件头" }
      if (count > budget) printf "%s: %s %d 行，上限 %d\n", file, kind, count, budget
    }
  ' "$1"
}

check_budget_ts() {
  awk -v file="$1" '
    { lines[NR] = $0 }
    END {
      if (NR == 0 || lines[1] !~ /^\/\*\*?/) exit
      count = 0
      for (i = 1; i <= NR; i++) {
        line = lines[i]
        if (line !~ /^\/\*\*?[[:space:]]*$/ && line !~ /^[[:space:]]*\*\/[[:space:]]*$/ && line !~ /^[[:space:]]*\*[[:space:]]*$/) count++
        if (line ~ /\*\//) break
      }
      if (count > 5) printf "%s: 文件头 %d 行，上限 5\n", file, count
    }
  ' "$1"
}

# ------------------------------------------------------- 会腐坏的引用

# 只收零误报的形态：文件行号、「第 N 行」、「N 行的」、「出现 N 次」、「line N」。
ROT_PATTERN='\.(go|ts|tsx|sql):[0-9]+|第[[:space:]]*[0-9]+[[:space:]]*行|[0-9][0-9.]*[kK]?[[:space:]]*行的|出现[[:space:]]*[0-9]+\+?[[:space:]]*次|\bline[[:space:]]+[0-9]+'

# ------------------------------------------------- 疑似历史叙述（仅提示）

HISTORY_PATTERN='此前|原来的|以前是|曾经|不再|重构前|旧的'

# ---------------------------------------------------------------- 执行

FILES=$(git ls-files 'internal/*.go' 'cmd/*.go' 'web/*.go' 'web/src/*.ts' 'web/src/*.tsx')

BUDGET_OUT=""
ROT_OUT=""
HISTORY_OUT=""

for f in $FILES; do
  [ -f "$f" ] || continue
  is_generated "$f" && continue

  case "$f" in
    *.go) out=$(check_budget_go "$f") ;;
    *) out=$(check_budget_ts "$f") ;;
  esac
  [ -n "$out" ] && BUDGET_OUT+="$out"$'\n'

  hits=$(grep -nE "^[[:space:]]*(//|\*|/\*)" "$f" | grep -E "$ROT_PATTERN")
  [ -n "$hits" ] && ROT_OUT+="$f: ${hits}"$'\n'

  hits=$(grep -cE "^[[:space:]]*(//|\*|/\*).*($HISTORY_PATTERN)" "$f")
  [ "$hits" -gt 0 ] && HISTORY_OUT+="$f ($hits)"$'\n'
done

if [ -n "$BUDGET_OUT" ]; then
  echo "注释头超预算："
  printf '%s' "$BUDGET_OUT"
  echo
  FAIL=1
fi

if [ -n "$ROT_OUT" ]; then
  echo "会腐坏的引用（行号 / 现状统计数字），改写成符号名："
  printf '%s' "$ROT_OUT"
  echo
  FAIL=1
fi

if [ -n "$HISTORY_OUT" ]; then
  echo "提示：以下文件含疑似历史叙述的词，按 docs/agents/doc-style.md 的三段判定逐条处置。"
  echo "（这一项误报多，不影响退出码。）"
  printf '%s' "$HISTORY_OUT"
  echo
fi

[ "$FAIL" -eq 0 ] && echo "文档风格检查通过。"
exit "$FAIL"
