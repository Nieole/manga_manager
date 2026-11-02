cd /c/Users/saber/dev/manga-refinement-48/web
E=../.tmp/retry
run() { local name=$1; shift; printf '%s\n' "$*" > $E/$name.command; "$@" > $E/$name.log 2>&1; echo $? > $E/$name.exit; }
run web-tests-json ../.tmp/node.exe node_modules/vitest/vitest.mjs run --pool=threads --maxWorkers=2 --exclude src/utils/heatmap.test.ts --reporter=json --outputFile=../.tmp/retry/web-results.json
run web-heatmap-json ../.tmp/node.exe node_modules/vitest/vitest.mjs run --pool=forks --maxWorkers=1 src/utils/heatmap.test.ts --reporter=json --outputFile=../.tmp/retry/heatmap-results.json
