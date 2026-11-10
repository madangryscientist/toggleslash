# frozen_string_literal: true

require_relative 'test_helper'

# Runs every shared evaluation vector and bucket pin from fixtures/evaluation/, read from the repository at
# test time (never a copy), through the same JSON parser the client uses.
class VectorsTest < Minitest::Test
  include TestSupport

  DIRECTORY = File.join(REPO_ROOT, 'fixtures', 'evaluation')
  COUNTS = { vectors: 0, pins: 0 } # rubocop:disable Style/MutableConstant

  Minitest.after_run do
    puts "vectors: #{COUNTS[:vectors]} pins: #{COUNTS[:pins]}"
  end

  def read(name)
    ToggleSlash::Json.parse(File.binread(File.join(DIRECTORY, name)))
  end

  def test_fixtures_exist
    names = Dir.children(DIRECTORY).select { |name| name.end_with?('.json') }
    refute_empty names, 'no fixture files were found'
  end

  Dir.children(DIRECTORY).sort.select { |name| name.end_with?('.json') && name != 'buckets.json' }.each do |name|
    define_method("test_vectors_#{name.delete_suffix('.json')}") do
      vectors = read(name).fetch('vectors')
      refute_empty vectors, "#{name} holds no vectors"
      vectors.each { |vector| check_vector(name, vector) }
    end
  end

  def check_vector(file, vector)
    ruleset = ToggleSlash::Eval::Rules.build({ 'flags' => vector['flags'], 'segments' => vector['segments'] })
    detail = ToggleSlash::Eval::Evaluator.evaluate(ruleset, vector['flagKey'], vector['context'])
    expected = vector['expected']
    COUNTS[:vectors] += 1
    assert same_json?(detail.wire, expected),
           "#{file}: #{vector['name']}\n  expected #{ToggleSlash::Json.generate(expected)}\n  got      #{ToggleSlash::Json.generate(detail.wire)}"
  end

  def test_bucket_pins
    pins = read('buckets.json').fetch('buckets')
    refute_empty pins, 'buckets.json holds no pins'
    pins.each do |pin|
      key = pin['flagKey']
      label = "buckets.json: #{key} #{pin['salt']} #{pin['contextKey']}"
      assert_equal pin['uint32'], ToggleSlash::Eval::Bucket.uint32(key, pin['salt'], pin['contextKey']),
                   "#{label} uint32"
      assert_equal pin['bucket'], ToggleSlash::Eval::Bucket.bucket(key, pin['salt'], pin['contextKey']),
                   "#{label} bucket"
      COUNTS[:pins] += 1
    end
  end
end
