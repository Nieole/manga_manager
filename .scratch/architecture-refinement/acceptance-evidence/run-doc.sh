cd /c/Users/saber/dev/manga-refinement-48
echo 'timeout 180 bash scripts/check-doc-style.sh' > .tmp/evidence/doc-style.command
timeout 180 bash scripts/check-doc-style.sh > .tmp/evidence/doc-style.log 2>&1
echo $? > .tmp/evidence/doc-style.exit
