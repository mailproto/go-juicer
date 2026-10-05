# Generates premailer's output for every fixture, for TestPremailerSemantics.
#
# This is the only thing that may write testdata/out/premailer. premailer
# re-serializes through libxml2, so these are compared by meaning (each
# element's style declarations), never byte for byte.
#
#   bundle exec ruby generate.rb           regenerate
#   bundle exec ruby generate.rb --check   verify committed outputs
require 'fileutils'
require 'json'
require 'nokogiri'
require 'premailer'

ROOT = File.expand_path('../..', __dir__)
IN = File.join(ROOT, 'in')
OUT = File.join(ROOT, 'out', 'premailer')
STAMP = "premailer #{Premailer::VERSION}\ncss_parser #{CssParser::VERSION}\nnokogiri #{Nokogiri::VERSION}\n"

check = ARGV.include?('--check')
drift = []
written = {}

Dir.glob('**/*.html', base: IN).sort.each do |rel|
  html = File.read(File.join(IN, rel), encoding: 'UTF-8')
  sidecar = File.join(IN, rel.sub(/\.html\z/, '.json'))
  opts = File.exist?(sidecar) ? (JSON.parse(File.read(sidecar))['options'] || {}) : {}
  args = { with_html_string: true, adapter: :nokogiri, input_encoding: 'UTF-8', warn_level: Premailer::Warnings::NONE }
  args[:css_string] = opts['extraCss'] if opts['extraCss']
  begin
    body = Premailer.new(html, **args).to_inline_css
    path = File.join(OUT, rel)
  rescue StandardError => e
    body = "#{e.class}: #{e.message.lines.first&.strip}\n"
    path = File.join(OUT, rel.sub(/\.html\z/, '.err'))
  end
  written[path] = true
  if check
    drift << rel unless File.exist?(path) && File.read(path, encoding: 'UTF-8') == body
  else
    FileUtils.mkdir_p(File.dirname(path))
    File.write(path, body)
  end
end

stamp = File.join(OUT, 'VERSION')
written[stamp] = true
if check
  drift << 'VERSION' unless File.exist?(stamp) && File.read(stamp) == STAMP
else
  File.write(stamp, STAMP)
end

Dir.glob('**/*', base: OUT).each do |rel|
  path = File.join(OUT, rel)
  next if File.directory?(path) || written[path]
  check ? drift << "#{rel} (stale)" : File.delete(path)
end

if check && !drift.empty?
  warn "premailer outputs out of date (#{drift.size}):\n  #{drift.join("\n  ")}"
  exit 1
end
puts "#{check ? 'verified' : 'wrote'} #{written.size - 1} premailer outputs (#{STAMP.lines.map(&:strip).join(', ')})"
