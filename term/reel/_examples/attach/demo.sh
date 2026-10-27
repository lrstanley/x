printf '\033[1;36m$\033[0m make test\n'
sleep 0.3
for pkg in reel reel/vt reel/render reel/sink; do
    printf '\033[33m…\033[0m %s' "$pkg"; sleep 0.25
    printf '\r\033[K\033[32m✔\033[0m %s\n' "$pkg"
done
sleep 0.2
printf '\033[1mPASS\033[0m 4 packages in 1.2s\n\033[1;36m$\033[0m '
sleep 0.5
