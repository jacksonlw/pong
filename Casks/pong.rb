# Homebrew cask for Pong. This repository is its tap:
#   brew tap jacksonlw/pong https://github.com/jacksonlw/pong
#   brew install --cask jacksonlw/pong/pong
# Each release rewrites version and sha256 (from Pong-<version>.zip.sha256).
cask "pong" do
  version "0.0.0" # PLACEHOLDER: release version
  sha256 "0000000000000000000000000000000000000000000000000000000000000000" # PLACEHOLDER

  url "https://github.com/jacksonlw/pong/releases/download/v#{version}/Pong-#{version}.zip"
  name "Pong"
  desc "Answers questions from coding agents from one place"
  homepage "https://pong.fly/"

  auto_updates true
  depends_on macos: :sequoia

  app "Pong.app"
  binary "#{appdir}/Pong.app/Contents/Helpers/pong"

  uninstall quit: "fly.pong.Pong"

  zap trash: "~/Library/Application Support/Pong"
end
