# typed: false
# frozen_string_literal: true

# This file was generated by GoReleaser. DO NOT EDIT.
class TerraformDemux < Formula
  desc "A user-friendly launcher (à la Bazelisk) for Terraform."
  homepage "https://github.com/etsy/terraform-demux"
  version "2.0.0"
  license "Apache-2.0"

  on_macos do
    if Hardware::CPU.intel?
      url "https://github.com/etsy/terraform-demux/releases/download/v2.0.0/terraform-demux_2.0.0_darwin_amd64.tar.gz"
      sha256 "00bef96b0ecafdcb09c15e99a1a1218c78dc3aff6acccbaaa135bb3902d2ddc7"

      def install
        bin.install "terraform-demux"
        bin.install_symlink bin/"terraform-demux" => "terraform"
      end
    end
    if Hardware::CPU.arm?
      url "https://github.com/etsy/terraform-demux/releases/download/v2.0.0/terraform-demux_2.0.0_darwin_arm64.tar.gz"
      sha256 "e7eff1f217f1807a6e94167a9fbe7c02ea471c9c91e24c547fe128d7f28e1221"

      def install
        bin.install "terraform-demux"
        bin.install_symlink bin/"terraform-demux" => "terraform"
      end
    end
  end

  on_linux do
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      url "https://github.com/etsy/terraform-demux/releases/download/v2.0.0/terraform-demux_2.0.0_linux_arm64.tar.gz"
      sha256 "dcc370cc6b7c651236645796fe4bee1bc22ccacb4ddda754037c5fd251e71649"

      def install
        bin.install "terraform-demux"
        bin.install_symlink bin/"terraform-demux" => "terraform"
      end
    end
    if Hardware::CPU.intel?
      url "https://github.com/etsy/terraform-demux/releases/download/v2.0.0/terraform-demux_2.0.0_linux_amd64.tar.gz"
      sha256 "0516bd7ad17cab3ef85763da64cca23dc919f0d8e5d49c84210b17c29542f5f8"

      def install
        bin.install "terraform-demux"
        bin.install_symlink bin/"terraform-demux" => "terraform"
      end
    end
  end

  conflicts_with "terraform"
end
