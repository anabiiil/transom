# Run with Homebrew's Ruby so validation uses its actual cask DSL:
# brew ruby packaging/homebrew/validate-cask.rb CASK ARM_ARCHIVE INTEL_ARCHIVE
require "cask/cask_loader"
require "digest"
require "simulate_system"
require "uri"

abort "Usage: brew ruby #{$PROGRAM_NAME} CASK ARM_ARCHIVE INTEL_ARCHIVE" unless ARGV.length == 3

content = File.read(ARGV[0])
abort "Unresolved release placeholders in cask" if content.match?(/@[A-Z_]+@/)

{ arm: ARGV[1], intel: ARGV[2] }.each do |arch, archive|
  Homebrew::SimulateSystem.with(os: :macos, arch: arch) do
    cask = Cask::CaskLoader::FromContentLoader.new(content).load(config: nil)
    expected_name = File.basename(archive)
    actual_name = File.basename(URI.parse(cask.url.to_s).path)
    abort "#{arch}: expected #{expected_name}, got #{actual_name}" unless actual_name == expected_name
    release = expected_name.match(/\Atransom-v(.+)-darwin-(?:arm64|amd64)\.tar\.gz\z/)
    unless release && cask.version.to_s == release[1]
      abort "#{arch}: cask version does not match the release archive"
    end

    expected_sha = Digest::SHA256.file(archive).hexdigest
    abort "#{arch}: release archive checksum mismatch" unless cask.sha256 == expected_sha

    unless cask.depends_on.macos && cask.depends_on.formula.empty? && cask.depends_on.cask.empty?
      abort "#{arch}: cask must require macOS and install without formula/cask dependencies"
    end
    unless cask.depends_on.macos.comparator == ">=" && cask.depends_on.macos.version.to_s == "12"
      abort "#{arch}: cask must support macOS Monterey and later"
    end

    binaries = cask.artifacts.select { |artifact| artifact.is_a?(Cask::Artifact::Binary) }
    unless binaries.length == 1 && binaries.first.to_a == ["transom"]
      abort "#{arch}: cask must install the transom binary"
    end

    puts "Validated Transom #{cask.version} for #{arch}: #{expected_name}"
  end
end
