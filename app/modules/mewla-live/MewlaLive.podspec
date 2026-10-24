require 'json'

package = JSON.parse(File.read(File.join(__dir__, 'package.json')))

Pod::Spec.new do |s|
  s.name           = 'MewlaLive'
  s.version        = package['version']
  s.summary        = 'Live Activity and widget state for Mewla'
  s.description    = 'Applies the daemon live_state to a Live Activity and the App Group cache the WidgetKit extension reads.'
  s.license        = 'Apache-2.0'
  s.author         = 'Mewla contributors'
  s.homepage       = 'https://github.com/daoleno/mewla'
  s.platforms      = { :ios => '16.4' }
  s.source         = { :path => '.' }
  s.static_framework = true
  s.source_files   = 'ios/**/*.swift'
  s.dependency 'ExpoModulesCore'
  s.frameworks = 'ActivityKit', 'WidgetKit'
  s.pod_target_xcconfig = { 'DEFINES_MODULE' => 'YES' }
end
