# frozen_string_literal: true

require_relative "test_helper"

# Every shared evaluation vector and every bucket pin (docs/evaluation.md, "The vectors").
class VectorsTest < Minitest::Test
  DIR = File.join(TestSupport::FIXTURES, "evaluation")
  TALLY = { vectors: 0, pins: 0 } # rubocop:disable Style/MutableConstant -- counted as the tests run

  def self.load(path)
    Toggleslash::Json.parse(File.binread(path))
  end

  # Deep equality that tells true from 1 and compares numbers as binary64.
  def same?(left, right)
    if [true, false].include?(left) || right == true || right == false
      left.equal?(right)
    elsif left.is_a?(Numeric) && right.is_a?(Numeric)
      left == right || (left.respond_to?(:nan?) && left.nan? && right.respond_to?(:nan?) && right.nan?)
    elsif left.is_a?(Hash) && right.is_a?(Hash)
      left.keys.sort == right.keys.sort && left.all? { |k, v| same?(v, right[k]) }
    elsif left.is_a?(Array) && right.is_a?(Array)
      left.size == right.size && left.zip(right).all? { |a, b| same?(a, b) }
    else
      left.instance_of?(right.class) && left == right
    end
  end

  def reason_hash(reason)
    out = { "kind" => reason.kind }
    out["code"] = reason.code unless reason.code.nil?
    out["ruleIndex"] = reason.rule_index unless reason.rule_index.nil?
    out["flagKey"] = reason.flag_key unless reason.flag_key.nil?
    out
  end

  def test_vectors
    paths = Dir[File.join(DIR, "*.json")].reject { |p| File.basename(p) == "buckets.json" }.sort
    refute_empty paths, "no vector files found"
    failures = []
    paths.each do |path|
      vectors = self.class.load(path)["vectors"]
      refute_empty vectors, "no vectors in #{File.basename(path)}"
      vectors.each do |vector|
        TALLY[:vectors] += 1
        message = check(vector)
        failures << "#{File.basename(path)}: #{vector["name"]}: #{message}" if message
      end
    end
    assert_empty failures, failures.first(10).join("\n")
  end

  def check(vector)
    ruleset = Toggleslash::Eval.load_ruleset("flags" => vector["flags"], "segments" => vector["segments"])
    value, index, reason = Toggleslash::Eval.evaluate(ruleset, vector["flagKey"], vector["context"])
    expected = vector["expected"]
    return "value #{value.inspect}, wanted #{expected["value"].inspect}" unless same?(value, expected["value"])
    unless index == expected["variationIndex"]
      return "index #{index.inspect}, wanted #{expected["variationIndex"].inspect}"
    end

    got = reason_hash(reason)
    "reason #{got.inspect}, wanted #{expected["reason"].inspect}" unless same?(got, expected["reason"])
  end

  def test_pins
    pins = self.class.load(File.join(DIR, "buckets.json"))["buckets"]
    refute_empty pins, "no pins found"
    pins.each do |pin|
      TALLY[:pins] += 1
      args = [pin["flagKey"], pin["salt"], pin["contextKey"]]
      assert_equal pin["uint32"], Toggleslash::Eval::Bucket.uint32(*args), args.inspect
      assert_equal pin["bucket"], Toggleslash::Eval::Bucket.bucket(*args), args.inspect
    end
  end

  Minitest.after_run { puts "vectors: #{TALLY[:vectors]} pins: #{TALLY[:pins]}" }
end
