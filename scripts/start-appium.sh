#!/bin/sh

set -eu

if [ "$(uname -s)" != "Darwin" ]; then
  echo "This launcher configures the Homebrew Android SDK for macOS." >&2
  exit 1
fi

brew_prefix=$(brew --prefix)
export ANDROID_HOME="${ANDROID_HOME:-$brew_prefix/share/android-commandlinetools}"
export ANDROID_SDK_ROOT="${ANDROID_SDK_ROOT:-$ANDROID_HOME}"
export JAVA_HOME="${JAVA_HOME:-$brew_prefix/opt/openjdk@21}"
export PATH="$ANDROID_HOME/platform-tools:$ANDROID_HOME/emulator:$JAVA_HOME/bin:$PATH"

if [ -z "${DEVELOPER_DIR:-}" ] && [ -x /Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild ]; then
  export DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer
fi

if [ -n "${DEVELOPER_DIR:-}" ] && [ ! -x "$DEVELOPER_DIR/usr/bin/xcodebuild" ]; then
  echo "DEVELOPER_DIR does not point to a usable Xcode installation: $DEVELOPER_DIR" >&2
  exit 1
fi

exec appium --address 127.0.0.1 --port 4723 "$@"
