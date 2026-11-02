cd /c/Users/saber/dev/manga-refinement-48/web
E=../.tmp/evidence
run() { local name=$1; shift; echo "$*" > $E/$name.command; "$@" > $E/$name.log 2>&1; echo $? > $E/$name.exit; }
run web-tests ../.tmp/node.exe node_modules/vitest/vitest.mjs run --pool=threads --maxWorkers=2 --exclude src/utils/heatmap.test.ts
run web-heatmap ../.tmp/node.exe node_modules/vitest/vitest.mjs run --pool=forks --maxWorkers=1 src/utils/heatmap.test.ts
run web-lint ../.tmp/node.exe node_modules/eslint/bin/eslint.js .
run web-tsc ../.tmp/node.exe node_modules/typescript/bin/tsc -b
run web-build ../.tmp/node.exe node_modules/vite/bin/vite.js build
