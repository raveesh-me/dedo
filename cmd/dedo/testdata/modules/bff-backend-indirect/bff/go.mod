module example.com/shop/bff

go 1.26

require (
	example.com/shop v0.0.0
	example.com/shop/lib v0.0.0
)

replace (
	example.com/shop => ../
	example.com/shop/lib => ../lib
)
