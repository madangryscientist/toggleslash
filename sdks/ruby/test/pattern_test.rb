# frozen_string_literal: true

require_relative "test_helper"

class PatternTest < Minitest::Test
  Pattern = Toggleslash::Eval::Pattern

  def match?(pattern, text)
    prog = Pattern.compile(pattern.unpack("U*"))
    prog ? Pattern.search(prog, text.unpack("U*")) : :invalid
  end

  def test_literals_classes_and_anchors
    assert_equal true, match?("ab", "xabx")
    assert_equal false, match?("^ab", "xab")
    assert_equal true, match?("ab$", "xab")
    assert_equal true, match?("[a-c]+", "zzb")
    assert_equal false, match?("[^a-c]", "abc")
    assert_equal true, match?("\\d{3}", "a123")
    assert_equal false, match?("a.c", "a\nc")
  end

  def test_alternation_and_counted_repeats
    assert_equal true, match?("^(ab|cd){2}$", "abcd")
    assert_equal false, match?("^(ab|cd){2}$", "ab")
    assert_equal true, match?("^a{2,}$", "aaaa")
    assert_equal false, match?("^a{2,3}$", "aaaa")
  end

  def test_unsupported_syntax_is_invalid
    ["(?:a)", "a**", "*a", "[a", "a{2,1}", "a{101}", "(a", "a)", "\\q", "[]", "{", "a{01}"].each do |pattern|
      assert_equal :invalid, match?(pattern, "a"), pattern
    end
  end

  def test_a_pattern_too_long_or_too_large_is_invalid
    assert_equal :invalid, match?("a" * 1025, "a")
    assert_equal :invalid, match?("((a{100}){100}){2}", "a")
  end

  def test_time_is_bounded_whatever_the_pattern
    started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
    assert_equal false, match?("^(a*)*$", "#{"a" * 5000}b")
    assert_operator Process.clock_gettime(Process::CLOCK_MONOTONIC) - started, :<, 5
  end

  def test_code_points_beyond_the_bmp
    assert_equal true, match?("^.$", "\u{1F600}")
  end
end
