# frozen_string_literal: true

Gem::Specification.new do |spec|
  spec.name = 'toggleslash'
  spec.version = '0.0.0'
  spec.summary = 'A ToggleSlash server SDK for Ruby (unpublished).'
  spec.authors = ['ToggleSlash']
  spec.license = 'Elastic-2.0'
  spec.required_ruby_version = '>= 3.3'
  spec.files = Dir['lib/**/*.rb']
  spec.require_paths = ['lib']
  spec.metadata['rubygems_mfa_required'] = 'true'
end
