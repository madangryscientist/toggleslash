# frozen_string_literal: true

Gem::Specification.new do |spec|
  spec.name = "toggleslash"
  spec.version = "0.0.0"
  spec.summary = "ToggleSlash server SDK for Ruby"
  spec.description = "Evaluates ToggleSlash feature flags locally against a ruleset it keeps current from the server."
  spec.authors = ["ToggleSlash"]
  spec.license = "Elastic-2.0"
  spec.required_ruby_version = ">= 3.3"
  spec.metadata["rubygems_mfa_required"] = "true"
  spec.files = Dir["lib/**/*.rb"] + ["README.md"]
  spec.require_paths = ["lib"]
end
