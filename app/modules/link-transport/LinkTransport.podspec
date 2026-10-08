require 'json'

package = JSON.parse(File.read(File.join(__dir__, 'package.json')))

Pod::Spec.new do |s|
  s.name           = 'LinkTransport'
  s.version        = package['version']
  s.summary        = 'Pinned TLS transport for Mewla Link'
  s.description    = 'Loopback-only L4 bridge that applies a Pairing V2 SPKI pin to every Mewla HTTP and WebSocket stream.'
  s.license        = 'Apache-2.0'
  s.author         = 'Mewla contributors'
  s.homepage       = 'https://github.com/daoleno/mewla'
  s.platforms      = { :ios => '16.4' }
  s.source         = { :path => '.' }
  s.static_framework = true
  s.source_files   = 'ios/**/*.swift'
  s.dependency 'ExpoModulesCore'
  s.frameworks = 'Network', 'Security', 'CryptoKit'
  s.pod_target_xcconfig = { 'DEFINES_MODULE' => 'YES' }
end
