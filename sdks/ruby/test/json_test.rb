# frozen_string_literal: true

require_relative "test_helper"

class JsonTest < Minitest::Test
  Json = Toggleslash::Json

  def test_scalars_and_containers
    assert_equal({ "a" => [1, 2.5, true, false, nil, "s"] }, Json.parse('{"a":[1,2.5,true,false,null,"s"]}'))
    assert_equal [], Json.parse(" [ ] ")
    assert_equal({}, Json.parse("{}"))
  end

  def test_bad_text_raises_parse_error
    ["", "{", "[1,]", "{\"a\":1,}", "01", "1.", "-", "tru", "\"\\x\"", "[1] 2", "'a'", "\"a\nb\""].each do |text|
      assert_raises(Json::ParseError, text.inspect) { Json.parse(text) }
    end
  end

  def test_numbers_are_binary64_without_warnings
    assert_equal Float::INFINITY, Json.parse("1e999")
    assert_equal(-Float::INFINITY, Json.parse("-1e999"))
    assert_equal 0.0, Json.parse("1e-999")
    assert_equal 1.0e22, Json.parse("1e22")
    assert_equal 9_223_372_036_854_775_808.0, Json.parse("9223372036854775808")
    assert_predicate Json.parse("-0"), :zero?
    assert_equal "-0.0", Json.parse("-0").to_s
    assert_equal 0.1, Json.parse("0.1")
    assert_equal 5, Json.parse("5")
  end

  def test_a_byte_order_mark_is_skipped
    assert_equal({ "a" => 1 }, Json.parse("\xEF\xBB\xBF{\"a\":1}".b))
  end

  def test_invalid_utf8_is_replaced
    assert_equal "a\uFFFDb", Json.parse("\"a\xFFb\"".b)
  end

  def test_the_last_duplicate_key_wins
    assert_equal({ "a" => 2 }, Json.parse('{"a":1,"a":2}'))
  end

  def test_escapes_and_surrogates
    assert_equal "\u00e9\n\"", Json.parse('"\u00e9\n\""')
    assert_equal "\u{1F600}", Json.parse('"\ud83d\ude00"')
    lone = Json.parse('"\ud83d"')
    assert_equal "\xED\xA0\xBD".b, lone.b
    assert_equal '"\ud83d"', Json.generate(lone)
  end

  def test_nesting_limit
    assert_equal 1000, Json::MAX_DEPTH
    assert_kind_of Array, Json.parse(("[" * 1000) + ("]" * 1000))
    assert_raises(Json::ParseError) { Json.parse(("[" * 1001) + ("]" * 1001)) }
    assert_raises(Json::ParseError) { Json.parse("[" * 100_000) }
  end

  def test_generate
    assert_equal '{"a":[1,2.5,true,null,"\u00e9\n"]}', Json.generate({ "a" => [1, 2.5, true, nil, "\u00e9\n"] })
    assert_equal "null", Json.generate(Float::NAN)
    assert_equal "1e999", Json.generate(Float::INFINITY)
    assert_equal '"\u0001\u007f"', Json.generate("\u0001\u007f")
  end

  def test_round_trip
    value = { "k" => ["x", 1, 2.25, { "z" => nil }, "\u{1F600}"] }
    assert_equal value, Json.parse(Json.generate(value))
  end
end
