# Homebrew cask for Pong. Published in the product owner's tap.
# PLACEHOLDERS, filled per release by scripts/release-wizard.sh: version, sha256 (from Pong-<version>.zip.sha256).
cask "pong" do
  version "0.0.0" # PLACEHOLDER: release version
  sha256 "0000000000000000000000000000000000000000000000000000000000000000" # PLACEHOLDER

  url "https://github.com/jacksonlw/pong/releases/download/v#{version}/Pong-#{version}.zip"
  name "Pong"
  desc "Answers questions from coding agents from one place on the Mac"
  homepage "https://pong.fly/"

  auto_updates true
  depends_on macos: ">= :sequoia"

  app "Pong.app"
  binary "#{appdir}/Pong.app/Contents/Helpers/pong"

  uninstall quit: "fly.pong.Pong"

  zap trash: "~/Library/Application Support/Pong"
end
