# typed: false
# frozen_string_literal: true

# This file was generated by GoReleaser. DO NOT EDIT.
class TerraformDemux < Formula
  desc "A user-friendly launcher (à la Bazelisk) for Terraform."
  homepage "https://github.com/etsy/terraform-demux"
  version "1.1.2"
  license "Apache-2.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/etsy/terraform-demux/releases/download/v1.1.2/terraform-demux_1.1.2_darwin_arm64.tar.gz"
      sha256 "ef19f8d928a654b20e502d157313ee3694eb0af536d553231bea9e119fd9bb91"

      def install
        bin.install "terraform-demux"
        bin.install_symlink bin/"terraform-demux" => "terraform"
      end
    end
    if Hardware::CPU.intel?
      url "https://github.com/etsy/terraform-demux/releases/download/v1.1.2/terraform-demux_1.1.2_darwin_amd64.tar.gz"
      sha256 "360ddf8b16eb498091fad37ad1aed6f078f74ee6eb01736332749a908da82f86"

      def install
        bin.install "terraform-demux"
        bin.install_symlink bin/"terraform-demux" => "terraform"
      end
    end
  end

  on_linux do
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      url "https://github.com/etsy/terraform-demux/releases/download/v1.1.2/terraform-demux_1.1.2_linux_arm64.tar.gz"
      sha256 "fa777ce183b2a57eea0216049cf69156a1e2252a6bd6e705ffeb56f14ed58f27"

      def install
        bin.install "terraform-demux"
        bin.install_symlink bin/"terraform-demux" => "terraform"
      end
    end
    if Hardware::CPU.intel?
      url "https://github.com/etsy/terraform-demux/releases/download/v1.1.2/terraform-demux_1.1.2_linux_amd64.tar.gz"
      sha256 "450cda34cd0479aa9cc6f183541574df50956aaa6a51ab53c571398e9ee08383"

      def install
        bin.install "terraform-demux"
        bin.install_symlink bin/"terraform-demux" => "terraform"
      end
    end
  end

  conflicts_with "terraform"
end
