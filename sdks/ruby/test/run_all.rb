# frozen_string_literal: true

# Loads every test file in this directory, so one process runs them all.
Dir[File.join(__dir__, "*_test.rb")].each { |file| require file }
