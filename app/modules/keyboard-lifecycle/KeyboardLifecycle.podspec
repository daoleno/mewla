require 'json'

package = JSON.parse(File.read(File.join(__dir__, 'package.json')))

Pod::Spec.new do |s|
  s.name           = 'KeyboardLifecycle'
  s.version        = package['version']
  s.summary        = 'Authoritative foreground IME and Composer focus snapshots for Mewla'
  s.description    = 'Reads current platform keyboard occlusion and native Composer focus for structured-chat lifecycle reconciliation.'
  s.license        = 'Apache-2.0'
  s.author         = 'Mewla contributors'
  s.homepage       = 'https://github.com/daoleno/mewla'
  s.platforms      = { :ios => '16.4' }
  s.source         = { :path => '.' }
  s.static_framework = true
  s.source_files   = 'ios/**/*.swift'
  s.dependency 'ExpoModulesCore'
  s.dependency 'React-Core'
  s.pod_target_xcconfig = { 'DEFINES_MODULE' => 'YES' }
end
