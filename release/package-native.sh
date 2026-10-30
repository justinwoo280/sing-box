# Source after initializing PACKAGE_FILES for fpm.
for library in libcronet.so libsing-lwip.so; do
  if [[ -f "dist/$library" ]]; then
    PACKAGE_FILES+=("dist/$library=/usr/bin/$library")
  fi
done
if [[ -d dist/licenses ]]; then
  PACKAGE_FILES+=(dist/licenses/=/usr/share/licenses/sing-box/)
fi
