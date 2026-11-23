#!/bin/bash
# Builds the Engine, the pong command-line tool and the Mac app, and assembles
# build/Pong.app (unsigned).
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
out="$root/build/Pong.app"

cd "$root/app"
swift build -c release
bin=$(swift build -c release --show-bin-path)

rm -rf "$out"
mkdir -p "$out/Contents/MacOS" "$out/Contents/Helpers"
cp "$bin/Pong" "$out/Contents/MacOS/Pong"
# Inter and the logos, from the brand kit; PongKit looks for its bundle in Contents/Resources.
mkdir -p "$out/Contents/Resources"
cp -R "$bin/Pong_PongKit.bundle" "$out/Contents/Resources/"
# The page, Mermaid script, tokens and font that draw a document's diagrams.
cp -R "$bin/Pong_PongDocument.bundle" "$out/Contents/Resources/"
# The Ping sounds, also beside the app itself: a system notification's sound must be in the main bundle.
cp "$root/app/Sources/PongKit/Resources/"ping-*.wav "$out/Contents/Resources/"
# The app icon, from app/AppIcon.svg (the brand kit's Ink icon), drawn by Quick Look and packed with iconutil.
tmp=$(mktemp -d)
qlmanage -t -s 1024 -o "$tmp" "$root/app/AppIcon.svg" >/dev/null
[ -f "$tmp/AppIcon.svg.png" ] || { echo "qlmanage did not render app/AppIcon.svg to a PNG" >&2; exit 1; }
mkdir "$tmp/Pong.iconset"
for s in 16 32 128 256 512; do
  sips -z $s $s "$tmp/AppIcon.svg.png" --out "$tmp/Pong.iconset/icon_${s}x${s}.png" >/dev/null
  sips -z $((s * 2)) $((s * 2)) "$tmp/AppIcon.svg.png" --out "$tmp/Pong.iconset/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns -o "$out/Contents/Resources/Pong.icns" "$tmp/Pong.iconset"
rm -rf "$tmp"
cd "$root"
go build -o "$out/Contents/Helpers/" ./cmd/pong-engine ./cmd/pong

cat > "$out/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key><string>fly.pong.Pong</string>
  <key>CFBundleName</key><string>Pong</string>
  <key>CFBundleIconFile</key><string>Pong</string>
  <key>CFBundleExecutable</key><string>Pong</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.1</string>
  <key>CFBundleVersion</key><string>1</string>
  <key>LSMinimumSystemVersion</key><string>15.0</string>
  <key>LSUIElement</key><true/>
  <key>NSMicrophoneUsageDescription</key><string>Pong listens only while its microphone is on, to write what you say into the box you are typing in, or to hear your answer in Voice mode.</string>
  <key>NSSpeechRecognitionUsageDescription</key><string>Pong turns what you say into text on this Mac. Nothing you say leaves it.</string>
</dict>
</plist>
EOF
echo "$out"
